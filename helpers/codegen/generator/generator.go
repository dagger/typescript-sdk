package generator

import "io/fs"

type GeneratedState struct {
	// Overlay is the overlay filesystem that contains generated code to write
	// over the output directory.
	Overlay fs.FS
}
