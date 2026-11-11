# Architecture

## The model

There is one source of truth and several projections.

```
   ┌─────────────────────────────────────────────┐
   │  the corpus                                 │
   │  user/ · lesson/ · project/ · reference/    │
   │                                             │
   │  one note per memory                        │
   │  YAML properties + the body in Markdown     │
   └──────────────────┬──────────────────────────┘
                      │
              smart-agents-memory index
                      │
        ┌─────────────┼─────────────┬──────────────┐
        ▼             ▼             ▼              ▼
   MiniMax        MiniMax          Qwen        the hot tier
   user.md        topics/        topics/         MEMORY.md
```

The corpus is authoritative. A projection exists so that each tool reads the
memory in the format it already expects, without losing its native
integration: MiniMax Code keeps injecting the profile in every session, and
Qwen Code keeps reading its topics.

## The layers

```
cmd/smart-agents-memory   the entrypoint
        │
internal/cli               the command line adapter
        │
internal/app               the use cases
        │
internal/vault             the port of the store
        │
internal/memory            the domain
internal/native            the projection formats
```

Two rules hold it together:

1. **The domain knows no gateway.** `internal/memory` and `internal/native`
   never import `internal/vault`.
2. **The port knows no gateway.** `internal/vault` never imports an adapter.

A gateway is one package that registers itself:

```go
func init() {
    vault.Register("sqlite", newStore)
}
```

The command imports the gateways for the side effect, so the registry is
complete before the first command runs.

## The port

```go
type Vault interface {
    Kind() string
    Root() string
    Entries(ctx context.Context) ([]memory.Entry, error)
    Create(ctx context.Context, e memory.Entry) (memory.Entry, error)
    Update(ctx context.Context, e memory.Entry) error
    Search(ctx context.Context, query string, limit int) ([]Hit, error)
    AppendLog(ctx context.Context, line string) error
    LogLines(ctx context.Context, limit int) ([]string, error)
    Ready(ctx context.Context) error
}
```

`Ready` is how a use case asks whether the store exists. It does not look
for a `corpus/` directory, and it does not branch on the gateway name.
Optional interfaces add what a gateway can do beyond the port:

| Interface | For | Implemented by |
|---|---|---|
| `Layout` | Creating the store | obsidian, disk, sqlite |
| `ProjectNotes` | The notes of a project | obsidian, disk, sqlite |
| `IndexWriter` | Publishing the index | obsidian, disk, sqlite |
| `Renamer` / `Remover` | Moving and deleting an entry | obsidian, disk, sqlite |
| `Diagnostics` | Notes that could not be read | obsidian, disk |

A gateway that does not implement one simply does not have that feature,
and the use case checks for it.

## The reconciliation rule

`index` runs in both directions.

**Import.** The native stores are read, and what they hold is brought into
the corpus. When a tool wrote straight into its own store, the fact is not
lost.

**Export.** The corpus is projected into the native stores. A projection is
never merged: it is regenerated.

**The tie.** When the corpus and a native store hold the same title with
different bodies, **the most recent change wins**. The modification time is
part of the entry, and every gateway supplies it: the file modification time
on disk, an `updated_at` column in SQLite. A projection has to be strictly
newer. The same second keeps the corpus, so a write that just landed is not
replaced by the projection it is about to regenerate.

**Orphans.** Export rewrites every projection from the corpus and deletes a
file no entry backs. `doctor` reports a topic that is not valid for the
runtime. `migrate` still removes the orphans of a legacy store.

**The hot tier.** `MEMORY.md` is a projection of the entries whose tier is
`hot`. Only the text between the markers is rewritten; the frontmatter
stays, because Cursor requires `alwaysApply: true`. A freeform block, with
no `###` headings, is imported once as a `user` entry titled `Profile`.

## Where the projections live

The projections are dictated by the tools, not by the store, so they are not
behind the port. They are always local, because the tools read them from the
local disk.

| Gateway | Root of the projections |
|---|---|
| `obsidian`, `disk` | `<store>/native/` — the store is a local folder |
| `sqlite` | `~/.local/share/smart-agents-memory/native/` |

The hot tier, `MEMORY.md`, follows the same rule: it sits at the root of the
store, or in the share directory when the store is not local.

## The tiers

| Tier | Where it goes | What it costs |
|---|---|---|
| `hot` | Injected in every session | Always in the context |
| `cold` | A topic file: only the name and the description | A line of metadata |

The MiniMax runtime injects the user profile, the name and the description
of every topic, and the tail of the agent memory. The body of a cold entry is
read on demand, through the topic reader or through `search`. That is the
difference between a context that grows with the memory and one that does
not.

## The installation

The installation is symlinks.

| Kind | Targets | Why |
|---|---|---|
| Directory | The four native stores | The runtime writes with a temporary file plus a rename, which replaces a symlink of a **file**. A symlink of a **directory** is crossed by the write. |
| File | The six global instruction slots | Those tools only read. |
| Directory | The eight skill directories | The skill is materialized once and linked everywhere. |

When a target exists and is not a symlink, a tool rewrote it: the previous
content is preserved with the `.bak-smart-agents-memory` suffix, and the link
is recreated. Nothing is deleted.
