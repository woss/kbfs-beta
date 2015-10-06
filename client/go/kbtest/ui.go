package kbtest

import (
	"fmt"

	keybase1 "github.com/keybase/client/go/protocol"
)

type gpgtestui struct {
	index int
}

func (g *gpgtestui) SelectKeyAndPushOption(arg keybase1.SelectKeyAndPushOptionArg) (keybase1.SelectKeyRes, error) {
	if len(arg.Keys) == 0 {
		return keybase1.SelectKeyRes{}, fmt.Errorf("no keys in arg")
	}
	if g.index >= len(arg.Keys) {
		return keybase1.SelectKeyRes{}, fmt.Errorf("test index %d outside bounds (num keys = %d)", g.index, len(arg.Keys))
	}
	key := arg.Keys[g.index]
	return keybase1.SelectKeyRes{KeyID: key.KeyID, DoSecretPush: true}, nil
}

func (g *gpgtestui) SelectKey(arg keybase1.SelectKeyArg) (string, error) {
	if len(arg.Keys) == 0 {
		return "", fmt.Errorf("no keys in arg")
	}
	if g.index >= len(arg.Keys) {
		return "", fmt.Errorf("test index %d outside bounds (num keys = %d)", g.index, len(arg.Keys))
	}
	key := arg.Keys[g.index]
	return key.KeyID, nil
}

func (g *gpgtestui) WantToAddGPGKey(dummy int) (bool, error) {
	return true, nil
}

func (g *gpgtestui) ConfirmDuplicateKeyChosen(dummy int) (bool, error) {
	return true, nil
}