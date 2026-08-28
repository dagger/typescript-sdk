package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdatePackageJSON(t *testing.T) {
	type testCase struct {
		name        string
		packageJSON string
		expected    string
	}

	for _, tc := range []testCase{
		{
			name:        "empty package.json",
			packageJSON: `{}`,
			expected:    `{"type": "module", "dependencies": {"typescript": "5.9.3"}}`,
		},
		{
			// The runtime mounts its prebuilt compiler only when the pin matches
			// its default, so a module that chose its own version keeps it and
			// accepts the install rather than being silently retargeted.
			name: "a user's own typescript pin is preserved",
			packageJSON: `{
  "type": "module",
  "dependencies": {
    "typescript": "5.4.0"
  }
}`,
			expected: `{
  "type": "module",
  "dependencies": {
    "typescript": "5.4.0"
  }
}`,
		},
		{
			// devDependencies is where a compiler normally goes. Adding
			// dependencies.typescript beside it would leave two declarations of the
			// same package, and npm resolves that to the runtime one — overriding
			// the version the module chose.
			name: "a user's own typescript devDependency is preserved",
			packageJSON: `{
  "type": "module",
  "devDependencies": {
    "typescript": "5.4.0"
  }
}`,
			expected: `{
  "type": "module",
  "devDependencies": {
    "typescript": "5.4.0"
  }
}`,
		},
		{
			name: "package.json with local dagger dependency is stripped",
			packageJSON: `{
  "type": "module",
  "dependencies": {
    "typescript": "5.9.3",
    "@dagger.io/dagger": "./sdk/index.ts"
  }
}`,
			expected: `{
  "type": "module",
  "dependencies": {
    "typescript": "5.9.3"
  }
}`,
		},
		{
			name: "package.json with local dagger dev dependency is stripped",
			packageJSON: `{
  "type": "module",
  "dependencies": {
    "typescript": "5.9.3"
  },
  "devDependencies": {
    "@dagger.io/dagger": "./sdk"
  }
}`,
			expected: `{
  "type": "module",
  "dependencies": {
    "typescript": "5.9.3"
  },
  "devDependencies": {}
}`,
		},
		{
			name: "package.json with comments has comments stripped",
			packageJSON: `{
  // Environment setup & latest features
  "type": "module",
  "dependencies": {
    // TypeScript
    "typescript": "5.9.3"
  }
} `,
			expected: `{
  "type": "module",
  "dependencies": {
    "typescript": "5.9.3"
  }
}`,
		},
		{
			name: "user scripts and metadata are preserved",
			packageJSON: `{
  "name": "user-pkg",
  "version": "1.2.3",
  "scripts": {
    "build": "tsc"
  }
}`,
			expected: `{
  "name": "user-pkg",
  "version": "1.2.3",
  "scripts": {
    "build": "tsc"
  },
  "type": "module",
  "dependencies": {
    "typescript": "5.9.3"
  }
}`,
		},
		{
			name:        "type=module already set still gains the typescript pin",
			packageJSON: `{"type": "module"}`,
			expected:    `{"type": "module", "dependencies": {"typescript": "5.9.3"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res, err := updatePackageJSON(removeJSONComments(tc.packageJSON))
			require.NoError(t, err)
			require.JSONEq(t, tc.expected, res)
		})
	}
}

func TestUpdateTSConfig(t *testing.T) {
	type testCase struct {
		name     string
		tsConfig string
		expected string
	}

	for _, tc := range []testCase{
		{
			name:     "empty tsconfig",
			tsConfig: `{}`,
			expected: `{
  "compilerOptions": {
    "experimentalDecorators": true,
    "paths": {
      "@dagger.io/dagger": ["./sdk/index.ts"],
      "@dagger.io/dagger/telemetry": ["./sdk/telemetry.ts"]
    }
  }
}`,
		},
		{
			name: "tsconfig with dagger paths already set is idempotent",
			tsConfig: `{
  "compilerOptions": {
    "paths": {
      "@dagger.io/dagger": ["./sdk/index.ts"],
      "@dagger.io/dagger/telemetry": ["./sdk/telemetry.ts"]
    }
  }
}`,
			expected: `{
  "compilerOptions": {
    "experimentalDecorators": true,
    "paths": {
      "@dagger.io/dagger": ["./sdk/index.ts"],
      "@dagger.io/dagger/telemetry": ["./sdk/telemetry.ts"]
    }
  }
}`,
		},
		{
			name: "tsconfig with user paths preserves them",
			tsConfig: `{
  "compilerOptions": {
    "target": "ES2020",
    "strict": true,
    "paths": {
      "@user/lib": ["./src/lib.ts"]
    }
  },
  "include": ["src/**/*"]
}`,
			expected: `{
  "compilerOptions": {
    "target": "ES2020",
    "strict": true,
    "experimentalDecorators": true,
    "paths": {
      "@user/lib": ["./src/lib.ts"],
      "@dagger.io/dagger": ["./sdk/index.ts"],
      "@dagger.io/dagger/telemetry": ["./sdk/telemetry.ts"]
    }
  },
  "include": ["src/**/*"]
}`,
		},
		{
			name: "tsconfig with comments has comments stripped",
			tsConfig: `{
  // Compiler settings
  "compilerOptions": {
    "target": "ES2020" // language target
  }
}`,
			expected: `{
  "compilerOptions": {
    "target": "ES2020",
    "experimentalDecorators": true,
    "paths": {
      "@dagger.io/dagger": ["./sdk/index.ts"],
      "@dagger.io/dagger/telemetry": ["./sdk/telemetry.ts"]
    }
  }
}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res, err := updateTSConfig(removeJSONComments(tc.tsConfig))
			require.NoError(t, err)
			require.JSONEq(t, tc.expected, res)
		})
	}
}

func TestUpdateDenoConfig(t *testing.T) {
	type testCase struct {
		name       string
		denoConfig string
		expected   string
	}

	for _, tc := range []testCase{
		{
			name:       "empty deno.json",
			denoConfig: `{}`,
			expected: `{
  "imports": {
    "typescript": "npm:typescript@5.9.3",
    "@dagger.io/dagger": "./sdk/index.ts",
    "@dagger.io/dagger/telemetry": "./sdk/telemetry.ts"
  },
  "nodeModulesDir": "auto",
  "compilerOptions": {
    "experimentalDecorators": true
  },
  "unstable": [
    "bare-node-builtins",
    "sloppy-imports",
    "node-globals",
    "byonm"
  ]
}`,
		},
		{
			name: "deno.json with dagger imports already set is idempotent",
			denoConfig: `{
  "imports": {
    "typescript": "npm:typescript@5.9.3",
    "@dagger.io/dagger": "./sdk/index.ts",
    "@dagger.io/dagger/telemetry": "./sdk/telemetry.ts"
  },
  "nodeModulesDir": "auto",
  "compilerOptions": {
    "experimentalDecorators": true
  },
  "unstable": [
    "bare-node-builtins",
    "sloppy-imports",
    "node-globals",
    "byonm"
  ]
}`,
			expected: `{
  "imports": {
    "typescript": "npm:typescript@5.9.3",
    "@dagger.io/dagger": "./sdk/index.ts",
    "@dagger.io/dagger/telemetry": "./sdk/telemetry.ts"
  },
  "nodeModulesDir": "auto",
  "compilerOptions": {
    "experimentalDecorators": true
  },
  "unstable": [
    "bare-node-builtins",
    "sloppy-imports",
    "node-globals",
    "byonm"
  ]
}`,
		},
		{
			name: "deno.json with partial unstable flags appends missing",
			denoConfig: `{
  "unstable": ["bare-node-builtins", "kv"]
}`,
			expected: `{
  "imports": {
    "typescript": "npm:typescript@5.9.3",
    "@dagger.io/dagger": "./sdk/index.ts",
    "@dagger.io/dagger/telemetry": "./sdk/telemetry.ts"
  },
  "nodeModulesDir": "auto",
  "compilerOptions": {
    "experimentalDecorators": true
  },
  "unstable": [
    "bare-node-builtins",
    "kv",
    "sloppy-imports",
    "node-globals",
    "byonm"
  ]
}`,
		},
		{
			// Deno has no node_modules to fall back on, so the compiler has to be
			// declared here — but a user who picked a version keeps it.
			name: "a user's own typescript import is preserved",
			denoConfig: `{
  "imports": {
    "typescript": "npm:typescript@5.4.0"
  }
}`,
			expected: `{
  "imports": {
    "typescript": "npm:typescript@5.4.0",
    "@dagger.io/dagger": "./sdk/index.ts",
    "@dagger.io/dagger/telemetry": "./sdk/telemetry.ts"
  },
  "nodeModulesDir": "auto",
  "compilerOptions": {
    "experimentalDecorators": true
  },
  "unstable": [
    "bare-node-builtins",
    "sloppy-imports",
    "node-globals",
    "byonm"
  ]
}`,
		},
		{
			name: "deno.json with user imports preserves them",
			denoConfig: `{
  "tasks": {
    "dev": "deno run main.ts"
  },
  "imports": {
    "@user/lib": "./src/lib.ts"
  }
}`,
			expected: `{
  "tasks": {
    "dev": "deno run main.ts"
  },
  "imports": {
    "typescript": "npm:typescript@5.9.3",
    "@user/lib": "./src/lib.ts",
    "@dagger.io/dagger": "./sdk/index.ts",
    "@dagger.io/dagger/telemetry": "./sdk/telemetry.ts"
  },
  "nodeModulesDir": "auto",
  "compilerOptions": {
    "experimentalDecorators": true
  },
  "unstable": [
    "bare-node-builtins",
    "sloppy-imports",
    "node-globals",
    "byonm"
  ]
}`,
		},
		{
			name: "deno.json with comments has comments stripped",
			denoConfig: `{
  // Environment
  "url": "https://foo/bar/baz.html" // A URL
}`,
			expected: `{
  "url": "https://foo/bar/baz.html",
  "imports": {
    "typescript": "npm:typescript@5.9.3",
    "@dagger.io/dagger": "./sdk/index.ts",
    "@dagger.io/dagger/telemetry": "./sdk/telemetry.ts"
  },
  "nodeModulesDir": "auto",
  "compilerOptions": {
    "experimentalDecorators": true
  },
  "unstable": [
    "bare-node-builtins",
    "sloppy-imports",
    "node-globals",
    "byonm"
  ]
}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res, err := updateDenoConfig(removeJSONComments(tc.denoConfig))
			require.NoError(t, err)
			require.JSONEq(t, tc.expected, res)
		})
	}
}

func TestReadInput(t *testing.T) {
	t.Parallel()

	t.Run("missing file returns empty object", func(t *testing.T) {
		t.Parallel()

		got, err := readInput(t.TempDir() + "/does-not-exist.json")
		require.NoError(t, err)
		require.JSONEq(t, `{}`, got)
	})

	t.Run("empty file returns empty object", func(t *testing.T) {
		t.Parallel()

		path := t.TempDir() + "/empty.json"
		require.NoError(t, os.WriteFile(path, []byte(""), 0o644))

		got, err := readInput(path)
		require.NoError(t, err)
		require.JSONEq(t, `{}`, got)
	})

	t.Run("whitespace-only file returns empty object", func(t *testing.T) {
		t.Parallel()

		path := t.TempDir() + "/blank.json"
		require.NoError(t, os.WriteFile(path, []byte("   \n\t  "), 0o644))

		got, err := readInput(path)
		require.NoError(t, err)
		require.JSONEq(t, `{}`, got)
	})

	t.Run("existing file is returned with comments stripped", func(t *testing.T) {
		t.Parallel()

		path := t.TempDir() + "/with-comments.json"
		require.NoError(t, os.WriteFile(path, []byte(`{
  // a comment
  "name": "demo"
}`), 0o644))

		got, err := readInput(path)
		require.NoError(t, err)
		require.JSONEq(t, `{"name": "demo"}`, got)
	})
}

func TestUpdateClientPackageJSON(t *testing.T) {
	type testCase struct {
		name          string
		packageJSON   string
		engineVersion string
		moduleName    string
		expected      string
	}

	for _, tc := range []testCase{
		{
			name:          "fresh client dir creates scoped package",
			packageJSON:   `{}`,
			engineVersion: "v0.18.0",
			moduleName:    "My Cool Module",
			expected:      `{"type":"module","name":"@dagger.io/my-cool-module-client","dependencies":{"@dagger.io/dagger":"0.18.0","typescript":"5.9.3"}}`,
		},
		{
			name:          "existing name is preserved, sdk dep is set, typescript kept",
			packageJSON:   `{"name":"@acme/existing","dependencies":{"typescript":"5.0.0"}}`,
			engineVersion: "v0.19.0-dev.abc123",
			moduleName:    "my-cool-module",
			expected:      `{"name":"@acme/existing","type":"module","dependencies":{"@dagger.io/dagger":"0.19.0-dev.abc123","typescript":"5.0.0"}}`,
		},
		{
			name:          "empty module name falls back to client",
			packageJSON:   `{}`,
			engineVersion: "0.20.0",
			moduleName:    "",
			expected:      `{"type":"module","name":"@dagger.io/client","dependencies":{"@dagger.io/dagger":"0.20.0","typescript":"5.9.3"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := updateClientPackageJSON(removeJSONComments(tc.packageJSON), tc.engineVersion, tc.moduleName)
			require.NoError(t, err)
			require.JSONEq(t, tc.expected, res)
		})
	}
}

func TestScopedClientName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"hello", "@dagger.io/hello-client"},
		{"My Cool Module", "@dagger.io/my-cool-module-client"},
		{"foo_bar.baz", "@dagger.io/foo-bar-baz-client"},
		{"--weird--", "@dagger.io/weird-client"},
		{"", "@dagger.io/client"},
	} {
		require.Equal(t, tc.want, scopedClientName(tc.in))
	}
}

func TestNpmVersion(t *testing.T) {
	require.Equal(t, "0.18.0", npmVersion("v0.18.0"))
	require.Equal(t, "0.18.0", npmVersion("0.18.0"))
	require.Equal(t, "0.19.0-dev.abc", npmVersion("v0.19.0-dev.abc"))
}

func TestUpdateClientPackageJSON_PreservesLocalDaggerRef(t *testing.T) {
	for _, tc := range []struct {
		name        string
		packageJSON string
		expected    string
	}{
		{
			name:        "file: ref preserved, not overwritten with version",
			packageJSON: `{"dependencies":{"@dagger.io/dagger":"file:../dagger2/sdk/typescript"}}`,
			expected:    `{"type":"module","name":"@dagger.io/hello-client","dependencies":{"@dagger.io/dagger":"file:../dagger2/sdk/typescript","typescript":"5.9.3"}}`,
		},
		{
			name:        "relative path ref preserved",
			packageJSON: `{"dependencies":{"@dagger.io/dagger":"./sdk"}}`,
			expected:    `{"type":"module","name":"@dagger.io/hello-client","dependencies":{"@dagger.io/dagger":"./sdk","typescript":"5.9.3"}}`,
		},
		{
			name:        "a version pin is refreshed to the engine version",
			packageJSON: `{"dependencies":{"@dagger.io/dagger":"0.9.0"}}`,
			expected:    `{"type":"module","name":"@dagger.io/hello-client","dependencies":{"@dagger.io/dagger":"1.0.0","typescript":"5.9.3"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := updateClientPackageJSON(removeJSONComments(tc.packageJSON), "v1.0.0", "hello")
			require.NoError(t, err)
			require.JSONEq(t, tc.expected, res)
		})
	}
}

func TestIsLocalDaggerRef(t *testing.T) {
	for _, v := range []string{"./sdk", "../sdk", "file:../x", "link:./x", "/abs/path", "workspace:*", "git+https://x", "https://x/y.tgz"} {
		require.True(t, isLocalDaggerRef(v), "%q should be local", v)
	}
	for _, v := range []string{"", "1.0.0", "^1.0.0", "~1.2.3", "1.0.0-beta.5", "latest", "*"} {
		require.False(t, isLocalDaggerRef(v), "%q should not be local", v)
	}
}
