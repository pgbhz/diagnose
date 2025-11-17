# Plano de Testes do Sistema de Teleodontologia Automatizada

## 1. Objetivo e Escopo

Este plano descreve como validamos o sistema de teleodontologia ponta a ponta, combinando chatbot Telegram, serviços Go, painel clínico em FastAPI e modelos de IA (LLM e visão computacional). O foco é garantir triagem segura, disponibilidade do serviço e rastreabilidade clínica sob LGPD.

**Componentes cobertos**:
- Núcleo conversacional em Go (`src/main.go`) com integrações de Telegram e pipeline diagnóstico.
- Cliente Gemini (`src/gemini/*.go`) para prompts multimodais.
- Dashboard clínico em FastAPI (`pyservice/app/main.py`) responsável por autenticação e visualização.
- Modelos de IA (LLM + CNN) e algoritmo híbrido de triagem.
- Infraestrutura de dados (configs JSON, assets de imagens, S3/PostgreSQL planejados).

## 2. Estratégia Geral em Camadas

Adotamos camadas complementares que cobrem desde funções isoladas até jornadas reais de pacientes.

| Camada | Objetivo | Artefatos principais | Frequência |
| --- | --- | --- | --- |
| Unidade Go | Validar regras do chatbot, FSM e gravação de diagnósticos | `src/main_test.go`, `src/gemini/client_test.go` | Cada PR |
| Unidade FastAPI | Garantir segurança básica e renderização do dashboard | `pyservice/tests/test_app.py` | Cada PR |
| Componentes de IA | Medir qualidade de NLP, visão e triagem | notebooks, pipelines de treino | Sprints de modelo |
| Integração | Exercitar fluxos texto + imagem entre serviços | `docker-compose`, mocks Gemini | Semanal ou alterações críticas |
| E2E e UX | Validar chatbot/dash com usuários/cypress | scripts Cypress + entrevistas | Releases piloto |

## 3. Testes de Unidade e Componentes

### 3.1. Núcleo Conversacional (Go)

Arquivo `src/main_test.go` cobre: carregamento do grafo de conversação (`TestLoadConversation`), fluxo completo de autenticação, upload de foto e wrap-up (`TestConversationFlow`) e persistência segura dos diagnósticos (`TestRecordDiagnosis`). São simuladas respostas do LLM e do classificador de imagens para garantir controle sobre side-effects.

**Comando**:

```bash
cd src
go test ./... -cover
```

### 3.2. Cliente Gemini API (Go)

`src/gemini/client_test.go` valida obrigatoriedade da chave (`TestNewClientRequiresAPIKey`), sucesso de chamadas HTTP, rejeição de prompts vazios, propagação de erros HTTP e envio de imagens como inline data. Testes usam `httptest` para garantir isolamento de rede e verificam o payload JSON enviado para a API do Gemini.

### 3.3. Persistência e Algoritmo de Triagem

`TestRecordDiagnosis` assegura que cada diagnóstico gravado contém caminho da foto, veredicto, justificativa e timestamp. O algoritmo híbrido de triagem herda esses registros para alimentar o score final e é validado com fixtures sintéticos (ver Seção 4.3).

### 3.4. Dashboard FastAPI (pytest)

`pyservice/tests/test_app.py` cobre:
- Proteção básica HTTP Basic (401 sem credenciais e logout forçando prompt).
- Listagem de pacientes apenas para superusers, escondendo racional clínico na visão geral.
- Visualização detalhada com histórico ordenado e link para foto.
- Endpoint `/refresh` recarregando `diagnosis.json` sem reiniciar o serviço.
- Bloqueio de usuários comuns ao dashboard administrativo.

**Comando**:

```bash
cd pyservice
pytest -q --maxfail=1 --disable-warnings
```

## 4. Testes dos Modelos de IA

### 4.1. Modelo de Processamento de Linguagem Natural

- **Datasets**: misto de relatos reais anonimizados e corpora odontológicos públicos; padronização terminológica (TUSS) e split 70/15/15.
- **Validação**: métricas ROUGE e BLEU (ROUGE-L minimo 0.65), revisão clínica qualitativa, cenários com queixas simples, múltiplas e descrições ambíguas.
- **Automação**: notebooks `notebooks/poc_occ.ipynb` geram relatórios e são exportados para PDF para revisão médica.

### 4.2. Modelo de Visão Computacional

- **Dataset principal**: Oral Cancer Dataset (Kaggle) com 950 imagens (500 positivas, 450 negativas) + augmentação (rotação, brilho, contraste, flip) e normalização.
- **Arquiteturas**: ResNet ou EfficientNet fine-tuned; backlog aberto para testar Vision Transformers.
- **Métricas alvo**: acuracia geral >= 0.75; para detecção de cancer, precisao >= 0.85, recall >= 0.90 e F1 >= 0.85.
- **Validação clínica**: bloco de 100 imagens revisadas manualmente para aferir falsos positivos/negativos críticos.

### 4.3. Algoritmo de Triagem Hibrido

- **Entrada**: dados estruturados do questionario, outputs do LLM e probabilidade do classificador.
- **Datasets**: casos reais e sintéticos classificados em Urgente, Moderado e Rotina por especialistas.
- **Validação**: fase 1 cobre todas as regras deterministicas (50 cenarios). Fase 2 usa ponderacao estatistica e exige Cohen's Kappa >= 0.70 e tempo de processamento < 30 s.

## 5. Testes de Integração

Executados via `docker-compose up --build` ou pipelines CI, conectando serviços Go, FastAPI, storage local (S3 fake) e mocks de IA.

| Cenário | Resultado esperado | Critérios de sucesso |
| --- | --- | --- |
| Questionario com 3 imagens | Pipeline gera resumo LLM, classifica fotos e grava score | Resposta em <= 30 s, registros exibidos no dashboard |
| Questionario sem imagem | Conversa finaliza com texto apenas | Aplicacao nao falha, diagnóstico marca exame pendente |
| Imagem de baixa qualidade | Classificador retorna estado "inconclusivo" | Bot notifica especialista e loga ocorrencia |
| Falha do serviço de IA | Mensagens ficam enfileiradas e estado do chat persiste | Retentativa automatica e nenhum dado perdido |
| Versionamento de roteiro | Paciente segue fluxo correto conforme `conversation.json` | Logs mostram versao aplicada |
| Caso urgente | Score acima do limiar dispara alertas | Notificacao em ate 2 minutos no canal de escalacao |

Checks adicionais: orquestração assíncrona, consistencia dos arquivos JSON, integridade de uploads em S3/local.

## 6. Testes End-to-End e Experiencia do Usuario

### 6.1. Chatbot

- Autenticacao (login/senha) e bloqueio de tentativas invalidas.
- Navegacao sequencial do questionario (tempo alvo <= 5 min).
- Upload de 1 a 5 fotos com feedback sobre formatos invalidos.
- Correcao de respostas e reinicio da conversa.
- Testes manuais guiados + script semi-automatizado para fluxos de happy path, dados faltantes e casos urgentes.

### 6.2. Dashboard e Observabilidade

- Automacao planejada com Cypress (testes de filtros, responsividade e acessibilidade WCAG 2.1).
- Smoke test diario via `pytest` + request para `/healthz` monitorado por uptime robot.

### 6.3. Satisfacao e Pesquisa com Usuarios

- Questionarios Likert, NPS e CSAT apos atendimentos piloto.
- Indicadores analisados por sprint para direcionar backlog de UX.

## 7. Automacao, Monitoracao e Qualidade Continua

- **CI**: workflow dispara `go test ./... -cover` e `pytest` a cada PR. Build falha se cobertura dos pacotes criticos < 80%.
- **Relatorios**: cobertura Go publicada via `go tool cover`; pytest gera `junitxml` arquivado no pipeline.
- **Infra local**: `docker-compose up --build` executa serviços, Gemini mock e banco temporario para testes manuais.
- **Observabilidade**: logs estruturados com IDs de conversa, dashboards de falhas e alertas para indisponibilidade do Gemini.

## 8. Cronograma e Responsabilidades

1. **Unidade/Componente**: contínuo por time de backend.
2. **Modelos de IA**: sprints dedicados (treino -> validação -> relatório clínico).
3. **Integração**: semanal ou quando houver mudança de contrato (Telegram, Gemini, FastAPI).
4. **E2E/UX**: antes de releases piloto e pós-incidentes.
5. **Revisão Executiva**: reunião mensal com indicadores de qualidade e riscos.

## 9. Critérios de Aceitação

- Cobertura de código >= 80% para núcleo Go e FastAPI.
- Todos os testes automatizados passando no CI.
- KPIs de IA dentro dos limiares definidos (Seção 4).
- Tempo de resposta ponta a ponta <= 30 s em fluxos com imagens.
- Satisfacao dos usuarios (SUS/NPS) >= 70.
- Zero incidentes de perda de dados em testes de resiliencia documentados.