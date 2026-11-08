// Command gencorpus writes the kilnfile test corpus: benign and hostile
// samples generated deterministically at test time, plus a manifest.json
// describing what the engine is expected to do with each one.
//
// Nothing here is functional malware: macros are dummy bytes, commands are
// inert strings and URLs use the .invalid TLD.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	out := flag.String("out", "testdata/generated", "output directory")
	large := flag.Bool("large", false, "produce full-size bombs (hundreds of MB to GB uncompressed, tens of MB on disk)")
	flag.Parse()
	if err := generate(*out, *large); err != nil {
		fmt.Fprintln(os.Stderr, "gencorpus:", err)
		os.Exit(1)
	}
}
