package libkbfs

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/protocol"
	"golang.org/x/net/context"
)

// TlfHandle uniquely identifies top-level folders by readers and
// writers.  It is go-routine-safe.
type TlfHandle struct {
	Readers     []keybase1.UID `codec:"r,omitempty"`
	Writers     []keybase1.UID `codec:"w,omitempty"`
	cachedName  string
	cachedBytes []byte
	cacheMutex  sync.Mutex // control access to the "cached" values
}

// NewTlfHandle constructs a new, blank TlfHandle.
func NewTlfHandle() *TlfHandle {
	return &TlfHandle{}
}

// TlfHandleDecode decodes b into a TlfHandle.
func TlfHandleDecode(b []byte, config Config) (*TlfHandle, error) {
	var handle TlfHandle
	err := config.Codec().Decode(b, &handle)
	if err != nil {
		return nil, err
	}

	return &handle, nil
}

func identifyUser(ctx context.Context, kbpki KBPKI, name, reason string,
	errCh chan<- error, results chan<- UserInfo) {
	// short-circuit if this is the special public user:
	if name == PublicUIDName {
		results <- UserInfo{
			Name: PublicUIDName,
			UID:  keybase1.PublicUID,
		}
		return
	}

	userInfo, err := kbpki.Identify(ctx, name, reason)
	if err != nil {
		select {
		case errCh <- err:
		default:
			// another worker reported an error before us; first one wins
		}
		return
	}
	results <- userInfo
}

// UIDList can be used to lexicographically sort UIDs.
type UIDList []keybase1.UID

func (u UIDList) Len() int {
	return len(u)
}

func (u UIDList) Less(i, j int) bool {
	return u[i].Less(u[j])
}

func (u UIDList) Swap(i, j int) {
	u[i], u[j] = u[j], u[i]
}

func sortedUIDsAndNames(m map[keybase1.UID]libkb.NormalizedUsername) (
	[]keybase1.UID, []string) {
	var uids []keybase1.UID
	var names []string
	for uid, name := range m {
		uids = append(uids, uid)
		names = append(names, name.String())
	}
	sort.Sort(UIDList(uids))
	sort.Sort(sort.StringSlice(names))
	return uids, names
}

func splitTLFNameIntoWritersAndReaders(name string) (
	writerNames, readerNames []string, err error) {
	splitNames := strings.SplitN(name, ReaderSep, 3)
	if len(splitNames) > 2 {
		return nil, nil, BadTLFNameError{name}
	}
	writerNames = strings.Split(splitNames[0], ",")
	if len(splitNames) > 1 {
		readerNames = strings.Split(splitNames[1], ",")
	}
	return writerNames, readerNames, nil
}

// normalizeUserNamesInTLF takes a split TLF name and, without doing
// any resolutions or identify calls, normalizes all elements of the
// name that are bare user names. It then returns the normalized name.
//
// Note that this normalizes (i.e., lower-cases) any assertions in the
// name as well, but doesn't resolve them.  This is safe since the
// libkb assertion parser does that same thing.
func normalizeUserNamesInTLF(writerNames, readerNames []string) string {
	sortedWriterNames := make([]string, len(writerNames))
	for i, w := range writerNames {
		sortedWriterNames[i] = libkb.NewNormalizedUsername(w).String()
	}
	sort.Strings(sortedWriterNames)
	normalizedName := strings.Join(sortedWriterNames, ",")
	if len(readerNames) > 0 {
		sortedReaderNames := make([]string, len(readerNames))
		for i, r := range readerNames {
			sortedReaderNames[i] =
				libkb.NewNormalizedUsername(r).String()
		}
		sort.Strings(sortedReaderNames)
		normalizedName += ReaderSep + strings.Join(sortedReaderNames, ",")
	}
	return normalizedName
}

// identifyTlfHandle parses a TlfHandle from a split TLF name.
func identifyTlfHandle(ctx context.Context, kbpki KBPKI,
	name string, public bool,
	writerNames, readerNames []string) (*TlfHandle, string, error) {
	if public && len(readerNames) > 0 {
		panic("public folder cannot have reader names")
	}

	// parallelize the resolutions for each user
	errCh := make(chan error, 1)
	wc := make(chan UserInfo, len(writerNames))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, user := range writerNames {
		reason := fmt.Sprintf("To confirm %s is a writer of folder %s", user, name)
		go identifyUser(ctx, kbpki, user, reason, errCh, wc)
	}

	rc := make(chan UserInfo, len(readerNames))
	for _, user := range readerNames {
		reason := fmt.Sprintf("To confirm %s is a reader of folder %s", user, name)
		go identifyUser(ctx, kbpki, user, reason, errCh, rc)
	}

	usedWNames := make(map[keybase1.UID]libkb.NormalizedUsername, len(writerNames))
	usedRNames := make(map[keybase1.UID]libkb.NormalizedUsername, len(readerNames))
	for i := 0; i < len(writerNames)+len(readerNames); i++ {
		select {
		case err := <-errCh:
			return nil, "", err
		case userInfo := <-wc:
			usedWNames[userInfo.UID] = userInfo.Name
		case userInfo := <-rc:
			usedRNames[userInfo.UID] = userInfo.Name
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}

	for uid := range usedWNames {
		delete(usedRNames, uid)
	}

	writerUIDs, writerNames := sortedUIDsAndNames(usedWNames)

	canonicalName := strings.Join(writerNames, ",")
	var cachedName string

	var readerUIDs []keybase1.UID
	if public {
		readerUIDs = []keybase1.UID{keybase1.PublicUID}
		// Public folders have the same canonical name as
		// their non-public equivalents.
		cachedName = canonicalName + ReaderSep + PublicUIDName
	} else {
		var readerNames []string
		readerUIDs, readerNames = sortedUIDsAndNames(usedRNames)
		if len(readerNames) > 0 {
			canonicalName += ReaderSep + strings.Join(readerNames, ",")
		}
		cachedName = canonicalName
	}

	h := &TlfHandle{
		Writers:    writerUIDs,
		Readers:    readerUIDs,
		cachedName: cachedName,
	}

	return h, canonicalName, nil
}

// IsPublic returns whether or not this TlfHandle represents a public
// top-level folder.
func (h *TlfHandle) IsPublic() bool {
	return len(h.Readers) == 1 && h.Readers[0].Equal(keybase1.PublicUID)
}

// IsPrivateShare returns whether or not this TlfHandle represents a
// private share (some non-public directory with more than one writer).
func (h *TlfHandle) IsPrivateShare() bool {
	return !h.IsPublic() && len(h.Writers) > 1
}

// HasPublic represents whether this top-level folder should have a
// corresponding public top-level folder.
func (h *TlfHandle) HasPublic() bool {
	return len(h.Readers) == 0
}

func (h *TlfHandle) findUserInList(user keybase1.UID,
	users []keybase1.UID) bool {
	// TODO: this could be more efficient with a cached map/set
	for _, u := range users {
		if u == user {
			return true
		}
	}
	return false
}

// IsWriter returns whether or not the given user is a writer for the
// top-level folder represented by this TlfHandle.
func (h *TlfHandle) IsWriter(user keybase1.UID) bool {
	return h.findUserInList(user, h.Writers)
}

// IsReader returns whether or not the given user is a reader for the
// top-level folder represented by this TlfHandle.
func (h *TlfHandle) IsReader(user keybase1.UID) bool {
	return h.IsPublic() || h.findUserInList(user, h.Readers) || h.IsWriter(user)
}

func resolveUids(ctx context.Context, config Config,
	uids []keybase1.UID) string {
	names := make([]string, 0, len(uids))
	// TODO: parallelize?
	for _, uid := range uids {
		if uid.Equal(keybase1.PublicUID) {
			// PublicUIDName is already normalized.
			names = append(names, PublicUIDName)
		} else if name, err := config.KBPKI().GetNormalizedUsername(ctx, uid); err == nil {
			names = append(names, string(name))
		} else {
			config.Reporter().Report(RptE, WrapError{err})
			names = append(names, fmt.Sprintf("uid:%s", uid))
		}
	}

	sort.Strings(names)
	return strings.Join(names, ",")
}

// ToString returns a string representation of this TlfHandle.
func (h *TlfHandle) ToString(ctx context.Context, config Config) string {
	h.cacheMutex.Lock()
	defer h.cacheMutex.Unlock()
	if h.cachedName != "" {
		// TODO: we should expire this cache periodically
		return h.cachedName
	}

	h.cachedName = resolveUids(ctx, config, h.Writers)

	// assume only additional readers are listed
	if len(h.Readers) > 0 {
		h.cachedName += ReaderSep + resolveUids(ctx, config, h.Readers)
	}

	// TODO: don't cache if there were errors?
	return h.cachedName
}

// ToBytes marshals this TlfHandle.
func (h *TlfHandle) ToBytes(config Config) (out []byte, err error) {
	h.cacheMutex.Lock()
	defer h.cacheMutex.Unlock()
	if len(h.cachedBytes) > 0 {
		return h.cachedBytes, nil
	}

	if out, err = config.Codec().Encode(h); err != nil {
		h.cachedBytes = out
	}
	return out, err
}

// ToKBFolder converts a TlfHandle into a keybase1.Folder,
// suitable for KBPKI calls.
func (h *TlfHandle) ToKBFolder(ctx context.Context, config Config) keybase1.Folder {
	return keybase1.Folder{
		Name:    h.ToString(ctx, config),
		Private: !h.IsPublic(),
	}
}

// Equal returns true if two TlfHandles are equal.
func (h *TlfHandle) Equal(rhs *TlfHandle, config Config) bool {
	hBytes, _ := h.ToBytes(config)
	rhsBytes, _ := rhs.ToBytes(config)
	return bytes.Equal(hBytes, rhsBytes)
}

// Users returns a list of all reader and writer UIDs for the tlf.
func (h *TlfHandle) Users() []keybase1.UID {
	var users []keybase1.UID
	for _, uid := range h.Writers {
		users = append(users, uid)
	}
	for _, uid := range h.Readers {
		users = append(users, uid)
	}
	return users
}

// ParseTlfHandle parses a TlfHandle from an encoded string. See
// ToString for the opposite direction.
func ParseTlfHandle(
	ctx context.Context, kbpki KBPKI, name string, public bool) (
	*TlfHandle, error) {
	// Before parsing the tlf handle (which results in identify
	// calls that cause tracker popups), first see if there's any
	// quick normalization of usernames we can do.  For example,
	// this avoids an identify in the case of "HEAD" which might
	// just be a shell trying to look for a git repo rather than a
	// real user lookup for "head" (KBFS-531).  Note that the name
	// might still contain assertions, which will result in
	// another alias in a subsequent lookup.
	writerNames, readerNames, err := splitTLFNameIntoWritersAndReaders(name)
	if err != nil {
		return nil, err
	}

	hasPublic := len(readerNames) == 0

	if public && !hasPublic {
		// No public folder exists for this folder.
		return nil, NoSuchNameError{Name: name}
	}

	normalizedName := normalizeUserNamesInTLF(writerNames, readerNames)
	if normalizedName != name {
		return nil, TlfNameNotCanonical{name, normalizedName}
	}

	currentUID, err := kbpki.GetCurrentUID(ctx)
	if err != nil {
		return nil, err
	}

	canRead := false
	if public {
		canRead = true
	} else {
		for _, writerName := range append(writerNames, readerNames...) {
			uid, err := kbpki.Resolve(ctx, writerName)
			if err != nil {
				return nil, err
			}
			if uid == currentUID {
				canRead = true
				break
			}
		}
	}

	if !canRead {
		var user string
		username, err := kbpki.GetNormalizedUsername(ctx, currentUID)
		if err == nil {
			user = username.String()
		} else {
			user = "uid:" + currentUID.String()
		}
		return nil, ReadAccessError{user, name}
	}

	h, canonicalName, err := identifyTlfHandle(
		ctx, kbpki, name, public, writerNames, readerNames)
	if err != nil {
		return nil, err
	}

	if canonicalName != name {
		return nil, TlfNameNotCanonical{name, canonicalName}
	}

	return h, nil
}