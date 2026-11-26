//go:build embed_payload

package main

import _ "embed"

//go:embed payload/rolerelay.exe
var embeddedAppBinary []byte
