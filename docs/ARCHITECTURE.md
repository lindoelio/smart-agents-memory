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
}
```

Six methods: the minimum the use cases need. Three optional interfaces add
what only some gateways have:

| Interface | For | Implemented by |
|---|---|---|
| `ProjectNotes` | The notes of a project | obsidian, disk |
| `IndexWriter` | Publishing the index | obsidian, disk |
| `Renamer` / `Remover` | Moving and deleting an entry | obsidian, disk |

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
on disk, an `updated_at` column in SQLite, the `last_edited_time` of a
Notion page. Without this rule, a manual edit of the corpus would be
reverted by an older projection.

**Orphans.** The export only creates. A projection that no entry backs
would be injected by the runtime without a counterpart, so the `migrate`
command removes them and the `doctor` reports them.

## Where the projections live

The projections are dictated by the tools, not by the store, so they are not
behind the port. They are always local, because the tools read them from the
local disk.

| Gateway | Root of the projections |
|---|---|
| `obsidian`, `disk` | `<store>/native/` — the store is a local folder |
| `sqlite`, `notion` | `~/.local/share/smart-agents-memory/native/` |

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
