//go:build analytics && !lpr

package analytics

import (
	"encoding/json"
	"errors"
)

// errLPRNotCompiled is returned by the LPR stub Configure().
//
// The "lpr" name is registered even when the module is not compiled in
// so that PathConf parsing does not reject "lpr" as an unknown module;
// the failure is deferred to Configure(), which lets the path stay
// alive without analytics.
var errLPRNotCompiled = errors.New("LPR not compiled in (build with -tags='analytics lpr')")

type lprStub struct{}

func (lprStub) Name() string                            { return "lpr" }
func (lprStub) Configure(string, json.RawMessage) error { return errLPRNotCompiled }
func (lprStub) Process(*Frame) ([]Event, error)         { return nil, nil }
func (lprStub) Close() error                            { return nil }

func init() {
	RegisterModule("lpr", func() Module { return lprStub{} })
}
