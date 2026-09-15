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
		name       string
		tsConfig   string
		clientsDir string
		packaged   bool
		modules    []string
		expected   string
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
			name:       "packaged layout points every alias into the shared client directory",
			tsConfig:   `{}`,
			clientsDir: "../../clients",
			packaged:   true,
			modules:    []string{"hello", "test"},
			expected: `{
  "compilerOptions": {
    "experimentalDecorators": true,
    "paths": {
      "@dagger.io/dagger": ["../../clients/dagger/index.ts"],
      "@dagger.io/dagger/telemetry": ["../../clients/dagger/telemetry.ts"],
      "@dagger.io/hello": ["../../clients/hello/hello.gen.ts"],
      "@dagger.io/test": ["../../clients/test/test.gen.ts"]
    }
  }
}`,
		},
		{
			name: "packaged layout retargets embedded aliases in place",
			tsConfig: `{
  "compilerOptions": {
    "paths": {
      "@dagger.io/dagger": ["./sdk/index.ts"],
      "@dagger.io/dagger/telemetry": ["./sdk/telemetry.ts"],
      "@dagger.io/gone": ["./clients/gone.gen.ts"]
    }
  }
}`,
			clientsDir: ".dagger/clients",
			packaged:   true,
			modules:    []string{"kept"},
			expected: `{
  "compilerOptions": {
    "experimentalDecorators": true,
    "paths": {
      "@dagger.io/dagger": ["./.dagger/clients/dagger/index.ts"],
      "@dagger.io/dagger/telemetry": ["./.dagger/clients/dagger/telemetry.ts"],
      "@dagger.io/kept": ["./.dagger/clients/kept/kept.gen.ts"]
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
			name:       "tsconfig with modules adds one alias per module client",
			tsConfig:   `{}`,
			clientsDir: "clients",
			modules:    []string{"my-dep", "app"},
			expected: `{
  "compilerOptions": {
    "experimentalDecorators": true,
    "paths": {
      "@dagger.io/dagger": ["./sdk/index.ts"],
      "@dagger.io/dagger/telemetry": ["./sdk/telemetry.ts"],
      "@dagger.io/my-dep": ["./clients/my-dep.gen.ts"],
      "@dagger.io/app": ["./clients/app.gen.ts"]
    }
  }
}`,
		},
		{
			name: "tsconfig drops aliases for modules that left, keeps user paths and telemetry",
			tsConfig: `{
  "compilerOptions": {
    "paths": {
      "@user/lib": ["./src/lib.ts"],
      "@dagger.io/dagger": ["./sdk/index.ts"],
      "@dagger.io/dagger/telemetry": ["./sdk/telemetry.ts"],
      "@dagger.io/gone": ["./clients/gone.gen.ts"],
      "@dagger.io/kept": ["./clients/kept.gen.ts"]
    }
  }
}`,
			clientsDir: "clients",
			modules:    []string{"kept"},
			expected: `{
  "compilerOptions": {
    "experimentalDecorators": true,
    "paths": {
      "@user/lib": ["./src/lib.ts"],
      "@dagger.io/dagger": ["./sdk/index.ts"],
      "@dagger.io/dagger/telemetry": ["./sdk/telemetry.ts"],
      "@dagger.io/kept": ["./clients/kept.gen.ts"]
    }
  }
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

			res, err := updateTSConfig(removeJSONComments(tc.tsConfig), aliasLayout{clientsDir: tc.clientsDir, packaged: tc.packaged}, tc.modules)
			require.NoError(t, err)
			require.JSONEq(t, tc.expected, res)
		})
	}
}

func TestUpdateDenoConfig(t *testing.T) {
	type testCase struct {
		name       string
		denoConfig string
		clientsDir string
		packaged   bool
		modules    []string
		expected   string
	}

	packagedCase := testCase{
		name:       "packaged layout points import-map aliases into the shared client directory",
		denoConfig: `{}`,
		clientsDir: "../../clients",
		packaged:   true,
		modules:    []string{"hello"},
		expected: `{
  "imports": {
    "typescript": "npm:typescript@5.9.3",
    "@dagger.io/dagger": "../../clients/dagger/index.ts",
    "@dagger.io/dagger/telemetry": "../../clients/dagger/telemetry.ts",
    "@dagger.io/hello": "../../clients/hello/hello.gen.ts"
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
	}

	moduleCase := testCase{
		name: "deno.json with modules adds and prunes import-map aliases",
		denoConfig: `{
  "imports": {
    "@dagger.io/gone": "./clients/gone.gen.ts"
  }
}`,
		clientsDir: "clients",
		modules:    []string{"kept"},
		expected: `{
  "imports": {
    "typescript": "npm:typescript@5.9.3",
    "@dagger.io/dagger": "./sdk/index.ts",
    "@dagger.io/dagger/telemetry": "./sdk/telemetry.ts",
    "@dagger.io/kept": "./clients/kept.gen.ts"
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
	}

	for _, tc := range append([]testCase{moduleCase, packagedCase}, []testCase{
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
	}...) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res, err := updateDenoConfig(removeJSONComments(tc.denoConfig), aliasLayout{clientsDir: tc.clientsDir, packaged: tc.packaged}, tc.modules)
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
