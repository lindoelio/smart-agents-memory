# Migration

## Moving a store from the previous release

The previous release was called `memoria`: a Python CLI, a skill named
`memoria-compartilhada`, and a store with Portuguese folders.

The `migrate` command brings that store to the current layout and
vocabulary. It reports before it acts, and it is idempotent.

```bash
# See what would change.
smart-agents-memory migrate

# Do it.
smart-agents-memory migrate --apply

# Then reconcile and check.
smart-agents-memory index
smart-agents-memory doctor
```

## What it changes

| Before | After |
|---|---|
| `corpus/usuario/` | `corpus/user/` |
| `corpus/licao/` | `corpus/lesson/` |
| `corpus/projeto/` | `corpus/project/` |
| `corpus/referencia/` | `corpus/reference/` |
| `projetos/` | `projects/` |
| `registro/` | `log/` |
| `corpus/INDICE.md` | `corpus/INDEX.md` |
| `type: usuario` | `type: user` |
| `type: licao` | `type: lesson` |
| `type: projeto` | `type: project` |
| `type: referencia` | `type: reference` |
| `source: "memoria-cli"` | `source: "smart-agents-memory-cli"` |
| `<!-- memoria:hot:start -->` | `<!-- smart-agents-memory:hot:start -->` |

It also renames the entries whose title stayed in the previous language,
merges the entries that duplicate another one, and removes the projections
that no entry backs.

The folder names are changed by hand, before the command runs: the command
moves the content, not the folders.

## Moving the store to another machine

The store is a folder. Copy it, or let the sync of the vault carry it:

```bash
scp -r "~/Documents/MyVault/Smart Agents Memory" new-machine:~/memory/
smart-agents-memory --memory-home ~/memory install
```

With the Obsidian gateway, the discovery finds the vault on its own: the
command looks for a folder named `Smart Agents Memory` inside the iCloud
vaults of the machine.

## Changing the store of a machine

```bash
smart-agents-memory --memory-home /path/to/the/store path
```

To make it permanent, put it in the configuration file:

```json
{
  "vault": "obsidian",
  "memory_home": "/path/to/the/store"
}
```

The file lives in `~/.config/smart-agents-memory/config.json`.

## Moving to another gateway

The corpus travels. The `obsidian` and `disk` gateways read the same notes;
the `sqlite` gateway carries the same entries in a database.

```bash
smart-agents-memory --vault sqlite --database ~/memory.db add \
  --scope lesson --title "A lesson" --body "Rule... Why..."
```

To move an existing corpus, read it with one gateway and write it with the
other. The round trip is covered by the test suite, and it is lossless.

## Moving from another product

There is no import command for a store of another shape. The format is
simple on purpose: one note per memory, YAML properties, and the body. A
small script that reads the entries of the other product and writes them
with `smart-agents-memory add` is enough.

The entry format is described in [MEMORY_FORMAT.md](MEMORY_FORMAT.md).

## From 0.1 to 0.2

Two things leave, and the hot tier changes shape.

- A configuration with `"vault": "notion"` fails with an explicit error.
  The gateway was not verified against the Notion API, so it is not part of
  the product. Move the corpus to `obsidian`, `disk` or `sqlite` first.
- `~/.minimax/memories` is no longer linked. Nothing was written there.
- The first `index` rewrites the inside of the hot markers from the `hot`
  entries. A block with no `###` headings is imported once as `Profile`.
  The frontmatter of `MEMORY.md`, including `alwaysApply: true`, stays.
