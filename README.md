# MDShelf

MDShelf serves the Markdown files in a folder as a small, phone-friendly website.

## Build

```sh
go build -o mdshelf .
```

The executable contains the full web interface. It does not need a separate assets folder.

Release binaries are built with the latest stable Go toolchain. The source remains compatible with Go 1.22 and newer.

## Install a release

Each GitHub release contains archives for macOS, Linux, and Windows on AMD64 and ARM64, plus a `SHA256SUMS` file. After extracting an archive on macOS or Linux, install the executable with:

```sh
mkdir -p "$HOME/bin"
/usr/bin/install -m 755 mdshelf "$HOME/bin/mdshelf"
```

Using `install` is preferable to copying over a previously launched executable on macOS because it replaces the binary safely instead of modifying a signed Mach-O file in place.

Release binaries are not notarized or signed with commercial Apple or Microsoft certificates. macOS Gatekeeper or Windows SmartScreen may therefore warn about a downloaded binary. Verify `SHA256SUMS` and build from source if you do not want to approve an unsigned download.

## Use

Run the executable from the folder you want to read:

```sh
cd /path/to/notes
/path/to/mdshelf
```

You can also pass the folder as the first positional argument:

```sh
/path/to/mdshelf /path/to/notes
```

MDShelf uses port `7331` by default. Choose another port with `-port`:

```sh
/path/to/mdshelf -port 9123 /path/to/notes
```

Then open the local or network URL printed at startup. MDShelf listens on all network interfaces so another device can connect.

MDShelf finds `.md` and `.markdown` files in the folder and its subfolders. It ignores hidden files, hidden folders, and symbolic links. Relative links between Markdown files and local images work in the reader. Language-tagged fenced code blocks use server-side syntax highlighting with matching light and dark themes.

## Network access

MDShelf has no sign-in screen. Anyone who can reach its port can read the Markdown files it lists. Run it only on a network you trust and stop it when you finish.

## Development

```sh
go test -race ./...
go vet ./...
node --check web/app.js
```

## Publishing a release

Push `main`, then create and push a semantic version tag:

```sh
git push origin main
git tag -a v0.1.0 -m "MDShelf v0.1.0"
git push origin v0.1.0
```

The release workflow accepts `vMAJOR.MINOR.PATCH` tags only and verifies that the tagged commit belongs to `main`. It tests the code, scans known Go vulnerabilities, builds all supported archives, generates checksums, and creates the GitHub release.

## License

MDShelf is available under the [MIT License](LICENSE). Third-party notices for the released binary are in [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES).
