package tui

import "os"

// SPIKE: IKESU_KOI=1 turns on the swimming-koi prototype.
var koiEnabled = os.Getenv("IKESU_KOI") == "1"
