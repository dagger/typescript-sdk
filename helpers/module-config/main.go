// module-config edits Dagger module configuration written into a JSON config
// file (package.json or deno.json). get-* commands print the current value;
// set-*/unset-* commands edit the file in place, preserving unrelated keys.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run dispatches a subcommand. get-* commands print to stdout (no trailing
// newline). set-*/unset-* commands rewrite the file in place.
//
// usage: module-config <command> <file> [value]
func run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: module-config <command> <file> [value]")
	}
	cmd, path := args[0], args[1]

	input, err := readOrEmpty(path)
	if err != nil {
		return err
	}

	switch cmd {
	case "get-package-manager":
		fmt.Print(getPackageManager(input))
		return nil
	case "get-base-image":
		fmt.Print(getBaseImage(input))
		return nil
	case "set-package-manager":
		v, err := value(args)
		if err != nil {
			return err
		}
		out, err := setPackageManager(input, v)
		if err != nil {
			return err
		}
		return os.WriteFile(path, []byte(out), 0o644)
	case "set-base-image":
		v, err := value(args)
		if err != nil {
			return err
		}
		out, err := setBaseImage(input, v)
		if err != nil {
			return err
		}
		return os.WriteFile(path, []byte(out), 0o644)
	case "unset-package-manager":
		out, err := unsetPackageManager(input)
		if err != nil {
			return err
		}
		return os.WriteFile(path, []byte(out), 0o644)
	case "unset-base-image":
		out, err := unsetBaseImage(input)
		if err != nil {
			return err
		}
		return os.WriteFile(path, []byte(out), 0o644)
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func value(args []string) (string, error) {
	if len(args) < 3 {
		return "", fmt.Errorf("%s requires a value", args[0])
	}
	return args[2], nil
}

func readOrEmpty(path string) (string, error) {
	contents, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "{}", nil
	case err != nil:
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if len(bytes.TrimSpace(contents)) == 0 {
		return "{}", nil
	}
	return string(contents), nil
}
