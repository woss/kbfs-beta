// Auto-generated by avdl-compiler v1.1.0 (https://github.com/keybase/node-avdl-compiler)
//   Input file: avdl/update.avdl
//   Generated : Wed Mar 02 2016 17:02:28 GMT-0800 (PST)

package keybase1

import (
	rpc "github.com/keybase/go-framed-msgpack-rpc"
	context "golang.org/x/net/context"
)

type UpdateOptions struct {
	Version             string `codec:"version" json:"version"`
	Platform            string `codec:"platform" json:"platform"`
	DestinationPath     string `codec:"destinationPath" json:"destinationPath"`
	Source              string `codec:"source" json:"source"`
	URL                 string `codec:"URL" json:"URL"`
	Channel             string `codec:"channel" json:"channel"`
	Force               bool   `codec:"force" json:"force"`
	DefaultInstructions string `codec:"defaultInstructions" json:"defaultInstructions"`
	SignaturePath       string `codec:"signaturePath" json:"signaturePath"`
}

type UpdateResult struct {
	Update *Update `codec:"update,omitempty" json:"update,omitempty"`
}

type UpdateArg struct {
	Options UpdateOptions `codec:"options" json:"options"`
}

type UpdateCheckArg struct {
	Force bool `codec:"force" json:"force"`
}

type UpdateInterface interface {
	Update(context.Context, UpdateOptions) (UpdateResult, error)
	UpdateCheck(context.Context, bool) error
}

func UpdateProtocol(i UpdateInterface) rpc.Protocol {
	return rpc.Protocol{
		Name: "keybase.1.update",
		Methods: map[string]rpc.ServeHandlerDescription{
			"update": {
				MakeArg: func() interface{} {
					ret := make([]UpdateArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]UpdateArg)
					if !ok {
						err = rpc.NewTypeError((*[]UpdateArg)(nil), args)
						return
					}
					ret, err = i.Update(ctx, (*typedArgs)[0].Options)
					return
				},
				MethodType: rpc.MethodCall,
			},
			"updateCheck": {
				MakeArg: func() interface{} {
					ret := make([]UpdateCheckArg, 1)
					return &ret
				},
				Handler: func(ctx context.Context, args interface{}) (ret interface{}, err error) {
					typedArgs, ok := args.(*[]UpdateCheckArg)
					if !ok {
						err = rpc.NewTypeError((*[]UpdateCheckArg)(nil), args)
						return
					}
					err = i.UpdateCheck(ctx, (*typedArgs)[0].Force)
					return
				},
				MethodType: rpc.MethodCall,
			},
		},
	}
}

type UpdateClient struct {
	Cli rpc.GenericClient
}

func (c UpdateClient) Update(ctx context.Context, options UpdateOptions) (res UpdateResult, err error) {
	__arg := UpdateArg{Options: options}
	err = c.Cli.Call(ctx, "keybase.1.update.update", []interface{}{__arg}, &res)
	return
}

func (c UpdateClient) UpdateCheck(ctx context.Context, force bool) (err error) {
	__arg := UpdateCheckArg{Force: force}
	err = c.Cli.Call(ctx, "keybase.1.update.updateCheck", []interface{}{__arg}, nil)
	return
}
