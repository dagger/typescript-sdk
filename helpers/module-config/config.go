package main

import (
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// getPackageManager reads the top-level "packageManager" field from a
// package.json. Returns "" when the key is absent.
func getPackageManager(input string) string {
	return gjson.Get(input, "packageManager").String()
}

// setPackageManager writes the top-level "packageManager" field.
func setPackageManager(input, value string) (string, error) {
	return sjson.Set(input, "packageManager", value)
}

// unsetPackageManager removes the top-level "packageManager" field.
func unsetPackageManager(input string) (string, error) {
	return sjson.Delete(input, "packageManager")
}

// getBaseImage reads "dagger.baseImage" from either a package.json or a
// deno.json (the engine accepts the same key in both). Returns "" when absent.
func getBaseImage(input string) string {
	return gjson.Get(input, "dagger.baseImage").String()
}

// setBaseImage writes "dagger.baseImage", creating the nested "dagger" object
// when necessary.
func setBaseImage(input, value string) (string, error) {
	return sjson.Set(input, "dagger.baseImage", value)
}

// unsetBaseImage removes "dagger.baseImage" and prunes the "dagger" object
// when it would otherwise be left empty, so the file does not carry an empty
// table after the round-trip.
func unsetBaseImage(input string) (string, error) {
	out, err := sjson.Delete(input, "dagger.baseImage")
	if err != nil {
		return "", err
	}
	dagger := gjson.Get(out, "dagger")
	if dagger.IsObject() && len(dagger.Map()) == 0 {
		return sjson.Delete(out, "dagger")
	}
	return out, nil
}
