# Smart Agents Memory

Memória compartilhada para agentes de IA: **um corpus, many harnesses**.

OpenCode, MiniMax Code, Codex, Cursor, Qwen Code, Claude Code e o Gemini CLI
leem a mesma memória, no formato que cada um já espera. A memória é escrita
uma única vez, por uma CLI, e reconciliada nos dois sentidos — para que
nenhuma ferramenta guarde uma versão privada da verdade.

```
┌──────────────────────────────────────────────────────────────┐
│  o store — um corpus, a fonte única da verdade               │
│  user/ · lesson/ · project/ · reference/                     │
└───────────────┬──────────────────────────────────────────────┘
                │  smart-agents-memory index
                ▼
┌──────────────────────────────────────────────────────────────┐
│  as projeções — a mesma memória, no formato de cada tool     │
│  MiniMax Code · Qwen Code · …                               │
└──────────────────────────────────────────────────────────────┘
```

## A ferramenta e o store são coisas diferentes

A **ferramenta** é este repositório: uma CLI, uma skill de agente, e as
ligações que conectam suas ferramentas à sua memória.

O **store** é a sua memória: uma pasta de notas. Por padrão é um vault do
Obsidian, sincronizado pelo iCloud — você lê e edita à mão — e ele nunca
viaja para um repositório público.

| | |
|---|---|
| Ferramenta | [github.com/lindoelio/smart-agents-memory](https://github.com/lindoelio/smart-agents-memory) — MIT |
| Store | A sua pasta. Obsidian por padrão, e ela é privada. |

## Instalar

### Homebrew

```bash
brew install lindoelio/smart-agents-memory/smart-agents-memory
```

### GitHub Releases

Baixe o binário da sua plataforma nos
[releases](https://github.com/lindoelio/smart-agents-memory/releases) e
coloque no `PATH`.

### Do código

```bash
git clone https://github.com/lindoelio/smart-agents-memory.git
cd smart-agents-memory
go build -o smart-agents-memory ./cmd/smart-agents-memory
```

### Ligar as ferramentas

```bash
smart-agents-memory install
```

Isso liga as ferramentas ao store e materializa a skill. É idempotente, e
repara um link que alguma ferramenta sobrescreveu.

Também cria o store quando não existe — em `~/Smart Agents Memory` por
padrão — então uma máquina que acabou de instalar o binário fica pronta em
um comando. Aponte para outro lugar com `--memory-home`, ou com a chave
`memory_home` do arquivo de configuração:

```bash
smart-agents-memory --memory-home ~/Documents/MeuVault/Smart\ Agents\ Memory install
```

O store é uma pasta de notas Markdown. Coloque dentro do seu vault do
Obsidian se quiser ler e editar a memória à mão; aponte para um banco
SQLite, ou para um database do Notion, se preferir esses.

## Usar

```bash
smart-agents-memory search "neon"           # encontrar algo
smart-agents-memory brief                   # o perfil e as regras
smart-agents-memory brief --project meu-app # + as notas de um projeto
smart-agents-memory add --scope lesson \
  --title "Título curto" \
  --body "Regra... Por quê..."               # registrar algo durável
smart-agents-memory log                     # o que foi registrado, e por quem
smart-agents-memory index                   # reconciliar corpus <-> projeções
smart-agents-memory doctor                  # verificar a instalação
smart-agents-memory path                    # onde está o store
```

A superfície completa está na
[referência da CLI](skills/smart-agents-memory/references/cli-reference.md).

## Onde a memória mora

O store é um **gateway**, e você escolhe:

| Backend | O que é |
|---|---|
| `obsidian` | **O padrão.** Notas em um vault do Obsidian, com properties, tags e wikilinks. O índice é uma nota nativa do Obsidian. |
| `disk` | Uma pasta simples de notas Markdown, para quem não usa Obsidian. |
| `sqlite` | Um arquivo de banco, para uma memória que viaja com a máquina. |
| `notion` | Um database do Notion, para uma memória que mora junto das outras notas. |

```bash
smart-agents-memory --vault sqlite search "neon"
smart-agents-memory --vault notion brief
```

A escolha é configuração, não um fork:

```json
{
  "vault": "obsidian",
  "memory_home": "~/Documents/MeuVault/Smart Agents Memory"
}
```

## O que cada ferramenta recebe

| Ferramenta | O que recebe |
|---|---|
| MiniMax Code | O perfil, injetado em toda sessão, e as lições como topic files |
| Qwen Code | Seus tópicos, e o hot tier |
| OpenCode, Codex, Claude Code, Gemini CLI | O hot tier, pelo slot de instrução global |
| Cursor | O hot tier, por uma user rule |

A memória **não** é despejada no contexto. O runtime injeta o perfil e apenas
o nome e a descrição de cada tópico; o corpo de uma lição é lido quando a
ferramenta decide que precisa dele.

Dois canais fazem esse trabalho: a **skill**, que cada ferramenta descobre
pelo próprio mecanismo, e o **slot de instrução global**, que garante o
mínimo. Veja a
[referência de integrações](skills/smart-agents-memory/references/integrations.md).

## As regras da memória

- **O corpus é a fonte única da verdade.** As projeções são regeneradas,
  nunca mescladas. Quando as duas divergem, vence a mudança mais recente.
- **Sempre o porquê.** Uma entrada sem motivo não é reutilizável — ninguém
  vai saber quando ela se aplica.
- **Inglês por padrão.** A memória armazenada é escrita em inglês, incluindo
  o texto de cada entrada: mantém as notas nativas para os modelos, e é
  material de leitura em inglês para quem é dono do store. As respostas a
  essa pessoa continuam no idioma dela.
- **Hot e cold.** `hot` entra em toda sessão — o perfil e as regras
  permanentes, e só isso. `cold` é buscado sob demanda.

## Arquitetura

Ports and adapters, dos dois lados:

- Os **entrypoints** — a CLI hoje, um servidor MCP depois — são adapters
  sobre os mesmos casos de uso.
- Os **gateways** — o store — são adapters atrás de uma porta. O domínio
  nunca descobre qual está em uso.

```
cmd/smart-agents-memory   entrypoint
        │
internal/app              casos de uso: search, brief, add, index, install…
        │
internal/vault            a porta ───┬── obsidian   (padrão)
        │                             ├── disk
        │                             ├── sqlite
        │                             └── notion
internal/memory           o domínio: entradas, tipos, tiers, slugs
internal/native           os formatos de projeção de cada tool
```

O teste que importa é o round trip: o mesmo corpus passa por todos os
backends e volta igual.

Veja o [documento de arquitetura](docs/ARCHITECTURE.md).

## Desenvolvimento

```bash
go test ./...
go vet ./...
```

A suíte cobre o domínio, cada gateway, o round trip entre eles, os casos de
uso e a linha de comando. O gateway do Notion é testado contra uma API
falsa, então a suíte não precisa de token nem de rede.

## Documentação

| Documento | O que tem |
|---|---|
| [ARCHITECTURE.md](docs/ARCHITECTURE.md) | O modelo, as portas, a regra de reconciliação |
| [MEMORY_FORMAT.md](docs/MEMORY_FORMAT.md) | O formato de uma entrada |
| [INTEGRATIONS.md](docs/INTEGRATIONS.md) | O que cada ferramenta recebe, e por quê |
| [DISTRIBUTION.md](docs/DISTRIBUTION.md) | Os canais de release, e como publicar um |
| [MIGRATION.md](docs/MIGRATION.md) | Como mover um store de outro layout |

## Licença

MIT. Veja [LICENSE](LICENSE).
