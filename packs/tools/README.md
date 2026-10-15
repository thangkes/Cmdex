# Tool packs

A tool pack holds the commands of one program that is not part of the
system: nmap, python, java, git… Every pack here is built into Cmdex, so
nothing is downloaded while it runs. A pack joins the library only when the
user turns it on. Cmdex offers it when one of its `tools` turns up on this
computer's `PATH`, and the user can also turn it on by hand under
Library › Tool packs.

## File

A tool pack is an ordinary pack (see `../windows.json`) with three more
fields:

```json
{
  "name": "nmap",
  "title": {"en": "Nmap", "vi": "Nmap"},
  "tools": ["nmap"],
  "origin": "tldr-pages (CC BY 4.0), https://github.com/tldr-pages/tldr",
  "version": 1,
  "commands": [ ... ]
}
```

- `name`: unique among tool packs; the library shows commands as `pack:<name>`.
- `tools`: the program file names that bring the pack, without a path or
  extension (`nmap` finds `nmap.exe` on Windows).
- `origin`: where the content comes from, with its licence. It is shown to
  the user. Content taken from another project keeps that project's licence
  and credit.

The packs test checks every file. Each pack needs a unique name, a title, an
origin and its tools, and every command must be one Cmdex can store.

## Making a pack from tldr-pages

`tools/tldr2pack` turns tldr pages into a pack. It runs at build time only,
on a local copy of the tldr repository. The app never reads tldr itself.

```
git clone --depth 1 https://github.com/tldr-pages/tldr ../tldr
go run ./tools/tldr2pack -tldr ../tldr -page nmap -title Nmap -out packs/tools/nmap.json
```

## Vietnamese

Every text is written as `{"en": "...", "vi": ""}`. An empty `vi` falls back
to English in the app, so a half-translated pack still works. Translators
fill the `vi` fields by hand, and nothing is machine-translated at run time.

Running tldr2pack again on the same `-out` file keeps every translation and
`danger` level whose command (platform + template) is unchanged.
`-report` prints how many texts still lack Vietnamese.
