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
			expected:    `{"type": "module"}`,
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
  "type": "module"
}`,
		},
		{
			name:        "type=module already set is a no-op",
			packageJSON: `{"type": "module"}`,
			expected:    `{"type": "module"}`,
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
