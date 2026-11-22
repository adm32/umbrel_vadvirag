# Notes for Claude

Read `README.md` first. Its "Common jobs" table says which file to edit for menus, offers,
prices and fees, and "Where to look" maps every source file. Search `src/` before writing a
new function, because most helpers already exist.

- `vadvirag-*/` is what Umbrel installs (yml, compose, prebuilt `bin/`). `src/<app>/` is the Go
  source. Build with `sh src/<app>/build.sh` and never edit `bin/`.
- Every release raises the version in two places (see the README table), adds a one-line
  Hungarian release note at the top of `releaseNotes`, passes `./check.sh`, then gets
  committed and pushed. Run `git pull --rebase` before you start, because other sessions push here too.
- A change to `src/rendelesek/menu.json` or `fees.go` needs a website build as well, because
  `src/weboldal/menu-fallback.json` is generated from them.
- The weekly menu lives in `src/weboldal/ajanlatok.json`. Keep `ajanlat_test.go` passing and
  extend it when the format changes.
- Don't add external libraries, fonts or scripts, and keep comments in served files short,
  because speed matters.
