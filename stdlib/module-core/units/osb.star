load("//classes/go.star", "go_binary")

# The osb binary itself. Pure Go, CGO_ENABLED=0, single ./cmd/osb target.
# Pins to a tagged release; bump `version` alongside a CHANGELOG entry.
# On-device, `apk upgrade osb` from the project's feed swaps the binary
# baked into the image at flash time.
go_binary(
    name = "osb",
    version = "0.10.11",
    source = "https://github.com/osb/osb.git",
    tag = "v0.10.11",
    license = "Apache-2.0",
    description = "osb build system CLI",
)
