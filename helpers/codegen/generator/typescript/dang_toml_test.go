package typescriptgenerator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderDangToml(t *testing.T) {
	for _, tc := range []struct {
		name   string
		engine string
		fresh  bool
		want   string // the dagger version written; "" for no file
	}{
		{"fresh module: the running engine", "v1.0.0-beta.21", true, "v1.0.0-beta.21"},
		{"build metadata dropped", "v1.0.0-beta.22+42424242.dirty", true, "v1.0.0-beta.22"},
		{"no v prefix", "1.0.0-beta.21", true, "v1.0.0-beta.21"},
		{"existing module: the pre-cut view", "v1.0.0-beta.21", false, PreCutDaggerVersion},
		{"existing module on a later engine", "v1.0.0", false, PreCutDaggerVersion},
		{"dev build counts as its release", "v1.0.0-dev-0a53dcd", true, "v1.0.0-dev-0a53dcd"},
		{"older engine: no dang.toml", "v1.0.0-beta.20", true, ""},
		{"older engine, existing module", "v1.0.0-beta.18", false, ""},
		{"pre-1.0 engine", "v0.21.9", true, ""},
		{"unversioned engine", "devel", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderDangToml(tc.engine, tc.fresh)
			if tc.want == "" {
				require.Empty(t, got)
				return
			}
			require.Contains(t, got, "\ndang = \"2.1\"\n\n[imports.Dagger]\ndagger = \""+tc.want+"\"\n")
		})
	}
}
