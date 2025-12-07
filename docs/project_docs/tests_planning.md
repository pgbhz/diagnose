# Plano de Testes do Sistema de Teleodontologia Automatizada

## 1. Introdução

Este documento estabelece o plano de testes para o Sistema de Teleodontologia Automatizada via chatbot Telegram, desenvolvido para triagem odontológica remota utilizando componentes de IA.

**Componentes principais**:
- Modelo de Processamento de Linguagem Natural (LLM) - Gemini API
- Modelo de Visão Computacional (CNN) para análise de imagens orofaciais
- Algoritmo híbrido de triagem para priorização de casos
- Backend de integração com chatbot Telegram

## 2. Testes de Unidade Implementados

### 2.1. Cliente Gemini API (`src/gemini/client_test.go`)

**Casos de teste implementados**:
- `TestNewClientRequiresAPIKey`: Valida que cliente requer chave de API
- `TestAskSuccess`: Valida comunicação bem-sucedida com API
- `TestAskRejectsEmptyPrompt`: Valida rejeição de prompts vazios
- `TestAskHandlesHTTPError`: Valida tratamento de erros HTTP

**Cobertura**: Inicialização do cliente, validação de requisições, tratamento de respostas e erros.

### 2.2. Fluxo Conversacional (`src/main_test.go`)

**Casos de teste implementados**:
- `TestLoadConversation`: Valida carregamento de arquivo de conversação JSON
- `TestConversationFlow`: Valida fluxo completo de interação (início → questões → fim → reinício)

**Cobertura**: Gerenciamento de estado de chat, persistência de respostas, navegação entre nós de conversação.

## 3. Testes do Modelo de IA

### 3.1. Modelo de Processamento de Linguagem Natural

#### Datasets
- Dados reais coletados na fase piloto + datasets públicos odontológicos
- Anonimização conforme LGPD
- Padronização usando terminologia TUSS
- Divisão: 70% treino, 15% validação, 15% teste

#### Estratégias de Validação
- **Métricas**: ROUGE, BLEU (threshold mínimo ROUGE-L ≥ 0.65)
- **Avaliação humana**: Especialistas odontológicos comparam resumos
- **Casos de teste**: Queixas simples, múltiplas queixas, descrições ambíguas

### 3.2. Modelo de Visão Computacional

#### Datasets
- **Dataset principal**: Oral Cancer Dataset (Kaggle) - 950 imagens rotuladas
  - 500 imagens de câncer oral
  - 450 imagens de tecido não-canceroso
- **Tratamento**: Augmentação (rotação, brilho, contraste, flipping), normalização, balanceamento de classes
- **Arquiteturas**: ResNet ou EfficientNet pré-treinadas

#### Estratégias de Validação
- **Métricas gerais**: Acurácia ≥ 75%
- **Detecção de câncer oral**: Precisão ≥ 85%, Recall ≥ 90%, F1-score ≥ 0.85
- **Validação clínica**: Avaliação por especialistas em amostra de 100 imagens

### 3.3. Algoritmo de Triagem

#### Datasets
- Casos sintéticos e reais classificados em 3 níveis: Urgente, Moderado, Rotina
- Classificação manual por especialistas

#### Estratégias de Validação
- **Fase 1 (regras)**: 50 casos de teste, cobertura 100% dos critérios de urgência
- **Fase 2 (ponderado)**: Cohen's Kappa ≥ 0.70, tempo de processamento < 30s

## 4. Testes de Integração

### Casos de Teste de Integração

| Cenário | Resultado Esperado | Critérios de Sucesso |
|---------|-------------------|---------------------|
| Questionário + 3 imagens | Resumo LLM, análise de imagens, score de triagem | Processamento ≤ 30s, dados no dashboard |
| Questionário sem imagens | Resumo, análise vazia, triagem por texto | Sistema não falha, mensagem clara |
| Imagens de baixa qualidade | Modelo retorna "inconclusivo" | Notificação ao especialista |
| Serviço de IA indisponível | Dados persistidos, retry automatizado | Dados não perdidos, retomada automática |
| Versionamento de questionário | Casos direcionados por versão | Separação adequada |
| Caso urgente detectado | Score alto, marcado "Urgente" | Notificação em ≤ 2min |

**Validações**: Comunicação assíncrona entre serviços, persistência PostgreSQL/S3, orquestração de IA.

## 5. Testes de Interação com Usuário

### 5.1. Testes de Chatbot

| Cenário | Resultado Esperado | Critérios de Sucesso |
|---------|-------------------|---------------------|
| Autenticação | Solicitação de login/senha | Bloqueio sem autenticação |
| Navegação questionário | Perguntas sequenciais, confirmação | Fluxo intuitivo ≤ 5min |
| Upload de imagens | Confirmação, feedback | Aceitar 1-5 fotos, rejeitar formatos inválidos |
| Correção de respostas | Voltar/editar disponível | Dados corrigidos corretamente |

### 5.2. Testes de Dashboard

**Ferramentas**: Cypress para automação
- Testes de interface web
- Filtros e análises
- Compatibilidade cross-browser
- Testes em múltiplos viewports
- Acessibilidade (WCAG 2.1)

### 5.3. Avaliação de Satisfação

**Questionários aplicados**:
- Escala Likert
- Net Promoter Score (NPS)
- Customer Satisfaction Score (CSAT)

**Aspectos avaliados**: Facilidade de uso, clareza, desempenho.

## 6. Automação de Testes

### Ferramentas
- **Testes unitários Go**: `go test` com cobertura
- **Testes E2E Dashboard**: Cypress
- **CI/CD**: Integração no pipeline para testes de regressão automáticos

### Cobertura
- Testes de unidade para componentes críticos (Gemini client, fluxo conversacional)
- Testes de integração para orquestração de IA
- Testes E2E para interface web
- Testes de acessibilidade e performance

## 7. Cronograma de Execução

1. **Testes de Unidade**: Implementados e executados continuamente
2. **Testes de Modelo de IA**: Fase de treinamento e validação
3. **Testes de Integração**: Após deployment dos serviços
4. **Testes de Usuário**: Fase piloto com usuários reais
5. **Automação**: Integração contínua no pipeline CI/CD

## 8. Critérios de Aceitação

- Cobertura de código ≥ 80% para componentes críticos
- Todos os testes unitários passando
- Métricas de IA dentro dos thresholds definidos
- Satisfação dos usuários ≥ 70% (SUS/NPS)
- Tempo de resposta do sistema ≤ 30s