# AppTide — Project Plan

## Estado atual (v0.6.2)

AppTide é uma CLI para Windows que unifica o gerenciamento de pacotes em um único arquivo YAML declarativo. O usuário define sua stack de software uma vez e o apptide instala, atualiza ou remove tudo de uma vez usando os package managers nativos.

### O que funciona hoje

**Fontes suportadas**

| Fonte      | Install | Upgrade | Uninstall | Check | Versão pós-install |
|------------|:-------:|:-------:|:---------:|:-----:|:------------------:|
| winget     | ✓       | ✓       | ✓         | ✓     | ✓                  |
| chocolatey | ✓       | ✓       | ✓         | ✓     | ✓                  |
| scoop      | ✓       | ✓       | ✓         | ✓     | ✓                  |
| github     | ✓       | —       | —         | ✓     | —                  |

**Comandos disponíveis**

| Comando       | O que faz                                                              |
|---------------|------------------------------------------------------------------------|
| `install`     | Instala/atualiza tudo no config, com TUI ao vivo                       |
| `list`        | Lista todos os pacotes configurados (tabela ou JSON)                   |
| `validate`    | Valida o YAML (campos obrigatórios, fontes válidas)                    |
| `verify`      | Compara config com estado real do sistema, sem modificar nada          |
| `doctor`      | Verifica disponibilidade dos package managers e auto-instala se pedido |
| `export`      | Gera packages.yaml a partir dos pacotes instalados no sistema          |
| `init`        | Cria packages.yaml via template ou wizard interativo                   |
| `selfupdate`  | Atualiza o próprio binário a partir do GitHub Releases                 |

**Capacidades transversais**

- Config imports recursivos com detecção de ciclos
- Filtro por `--category` e `--source`
- Lifecycle hooks (`pre_install`, `post_install`)
- `--dry-run` para simular sem executar
- `--force` para forçar reinstalação
- Saída JSON estruturada para CI/CD
- `no_upgrade: true` para fixar pacotes sem upgrades automáticos
- Seleção inteligente de assets do GitHub (scoring + glob override)
- Gerenciamento automático de PATH (`--add-to-path`)

---

## O que falta / limitações conhecidas

### Lacunas funcionais

1. **GitHub não tem uninstall**
   Retorna erro explícito. O usuário precisa remover o binário manualmente.

2. **GitHub não detecta versão pós-instalação**
   `Check()` retorna versão vazia; o TUI exibe "installed" sem número de versão.
   Causa: binário extraído pode ter nome arbitrário; não há API local de versão.

3. **GitHub não tem upgrade nativo**
   Só sabe instalar. Se o pacote já está instalado, usa `ErrAlreadyInstalled` como no-op.
   Um upgrade real exigiria comparar a versão instalada com a tag mais recente na API.

4. **Extração de `.7z` exige 7-Zip externo no PATH**
   Se `7z.exe` ou `7za.exe` não estiver disponível, a instalação falha com mensagem de erro.

5. **`export` não produz config pronta para uso imediato**
   Gera YAML com IDs corretos mas sem campos como `description`, `no_upgrade`, hooks etc.
   É um ponto de partida, não um config completo.

6. **`selfupdate` não tem agendamento automático**
   Precisa ser disparado manualmente. Não há verificação silenciosa na inicialização.

7. **Nenhum mecanismo de rollback**
   Se uma instalação falha parcialmente, não há desfazer automático.

8. **Sem suporte a proxies HTTP**
   Clientes HTTP não configuram proxy. Ambientes corporativos com proxy podem falhar silenciosamente.

### Limitações de design

- **Windows-only**: código usa PowerShell, `cmd /C`, `msiexec.exe`, `%LOCALAPPDATA%` etc.
- **Sem resolução de dependências**: pacotes são tratados de forma atômica e independente.
- **Sem retry automático**: falha em um pacote não é retentada.
- **Sem versionamento semântico por range**: só aceita versão exata ou `latest`.

---

## Sugestões de melhoria

As sugestões estão organizadas por impacto e esforço estimado.

---

### Alta prioridade

#### 1. Upgrade e versão para `github`

**Problema:** Não é possível saber se um pacote GitHub está desatualizado nem atualizá-lo.

**Proposta:**
- `Check()` deve chamar a API do GitHub e comparar a tag mais recente com a versão armazenada localmente.
- Salvar a versão instalada em um arquivo de estado local (`~/.config/apptide/state.json` ou similar), keyed por `repo`.
- `Install()` quando já instalado: comparar versão local com última tag e atualizar se necessário, respeitando `no_upgrade`.

**Benefício:** Fecha a maior lacuna funcional do projeto.

---

#### 2. Uninstall para `github`

**Problema:** Binários instalados via GitHub releases não têm remoção automatizada.

**Proposta:**
- Para binários copiados: deletar `<install_dir>/<binary_name>.exe` e o diretório `<install_dir>/<binary_name>/` se existir.
- Para `run_installer: true`: não há como automatizar de forma confiável; emitir aviso claro e instruções manuais.
- Registrar o caminho instalado no arquivo de estado local (ver item 1).

---

#### 3. Arquivo de estado local

**Problema:** O apptide não persiste nenhuma informação sobre o que instalou.

**Proposta:**
- Criar `~/.config/apptide/state.json` (ou `%APPDATA%\apptide\state.json`).
- Estrutura mínima:
  ```json
  {
    "github/jesseduffield/lazygit": {
      "version": "v0.40.2",
      "install_dir": "C:\\...",
      "binary": "lazygit.exe",
      "installed_at": "2025-04-10T..."
    }
  }
  ```
- Habilita: upgrade real do GitHub, uninstall confiável, `verify` com versão real.

---

### Média prioridade

#### 4. Verificação de checksum para `github`

**Problema:** Releases do GitHub frequentemente publicam arquivos `.sha256` ao lado dos assets, mas o apptide não os valida.

**Proposta:**
- Campo `checksum` no bloco `github:` (opcional).
- Após download, validar antes de extrair.
- Alternativa automática: procurar arquivo `<asset>.sha256` no mesmo release e validar sem configuração do usuário.

---

#### 5. Verificação silenciosa de atualizações na inicialização

**Problema:** `selfupdate` é manual. O usuário pode ficar em versão antiga por muito tempo.

**Proposta:**
- Checagem assíncrona na inicialização (não bloqueia o comando principal).
- Se versão nova disponível: exibir aviso discreto no rodapé do output (`⚡ apptide v0.7.0 disponível — apptide selfupdate`).
- Controlado por flag `--no-update-check` e configurável para desabilitar.

---

#### 6. Suporte a proxy HTTP

**Problema:** Ambientes corporativos com proxy não funcionam.

**Proposta:**
- Respeitar variáveis `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` já no `http.DefaultTransport`.
- Adicionar flag `--proxy` no root command como override explícito.

---

#### 7. Retry automático em falhas transitórias

**Problema:** Falhas de rede ou timeout matam a instalação sem chance de recuperação.

**Proposta:**
- Retry com backoff exponencial para operações de download (GitHub releases).
- Máximo de 3 tentativas por padrão, configurável.
- Não aplicar retry em erros de configuração ou rate limit sem token.

---

### Baixa prioridade / melhorias de DX

#### 8. Comando `add` para inserção rápida de pacotes

**Problema:** Adicionar um pacote ao config é manual (editar YAML).

**Proposta:**
```
apptide add --name "Git" --source winget --id Git.Git --category Development
```
- Faz append no arquivo de config existente, preservando estrutura.
- Útil para fluxos de onboarding e automações.

---

#### 9. Saída `--output json` para `install`

**Problema:** `install` só tem TUI; não há saída JSON estruturada para CI/CD.
`verify` e `list` já têm JSON, mas `install` não.

**Proposta:**
- Quando `--output json`, desabilitar TUI e emitir linha-por-linha JSON (NDJSON) com resultado de cada pacote.
- Compatível com pipelines que precisam parsear resultado de instalações.

---

#### 10. Filtro de pacotes por tag/label

**Problema:** Não há como marcar pacotes como "dev-only", "work", "gaming" etc. e instalar apenas um subconjunto sem criar múltiplos arquivos.

**Proposta:**
- Campo `tags: [dev, minimal]` nos pacotes.
- Flag `--tags dev,minimal` no `install`/`verify`.
- Complementa (não substitui) o filtro por `--category`.

---

#### 11. Suporte a `.tar.xz` e `.tar.bz2`

**Problema:** Alguns releases do GitHub usam `.tar.xz` ou `.tar.bz2`. Hoje esses assets ficam com score 0 (excluídos).

**Proposta:**
- Adicionar decompressão para `xz` (usando `compress/xz` ou dependência externa) e `bz2` (já no stdlib via `compress/bzip2`).
- Incluir esses formatos no scoring de assets.

---

## Roadmap sugerido

```
v0.7.0 — Estado e versão para GitHub
  - Arquivo de estado local
  - Check com versão real para github
  - Upgrade real para github (sem no_upgrade)

v0.8.0 — Gestão completa do ciclo de vida
  - Uninstall para github (binários)
  - Checksum para github

v0.9.0 — Resiliência e automação
  - Retry com backoff exponencial
  - Verificação silenciosa de atualização na inicialização
  - Suporte a proxy HTTP

v1.0.0 — Polimento e DX
  - Saída JSON para install
  - Filtro por tags
  - Comando add
  - Suporte a .tar.xz / .tar.bz2
```

---

## Notas técnicas

- O campo `InfoURL` em `config.Package` está definido mas nunca consumido. Pode ser exibido no `list --output json` ou no futuro comando `add` como documentação do pacote.
- O `ErrAlreadyInstalled` é reutilizado como sentinel de "no-op" em uninstall (package not found); o TUI já trata corretamente com label "already uninstalled". Renomear para `ErrNoOp` seria mais semântico, mas exigiria atualizar todos os chamadores.
- O flag `-o` está registrado tanto no root (persistent, formato de saída) quanto em `export` e `init` (local, caminho de arquivo). Cobra resolve corretamente por prioridade local, mas pode surpreender usuários que tentem usar `-o json` em conjunto com `export -o file.yaml`.
