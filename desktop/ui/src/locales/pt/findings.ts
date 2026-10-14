// Portuguese dictionary for code-health findings. Must mirror
// en/findings.ts keys exactly.
export const findings = {
  intro: "Lido do código a cada varredura: antipadrões, logging ausente ou mal configurado, ajustes de debug que chegam à produção, segredos e mais. Achados que você aceitar ou marcar como falso positivo continuam fechados nas varreduras seguintes.",
  allComponents: "Todos os componentes",
  repositoryWide: "Repositório inteiro",
  openCount: "{count} abertos",
  allCategories: "Todas as categorias",
  occurrences: "{count} ocorrências",
  lines: "{count} linhas",
  moreLocations: "e mais {count}",
  showLocations: "Mostrar locais",
  hideLocations: "Ocultar locais",
  createTask: "Criar tarefa",
  createTaskSelected: "Criar tarefa para {count}",
  selectAll: "Selecionar tudo",
  clearSelection: "Limpar",
  accept: "Aceitar",
  falsePositive: "Falso positivo",
  reopen: "Reabrir",
  openTask: "Abrir {key}",
  reason: "Motivo: {reason}",
  resolvedHint: "A última varredura não encontra mais isto.",
  updateFailed: "Não foi possível atualizar o achado",
  componentSummary: {
    open: "{count} achados de saúde do código abertos",
    none: "Nenhum achado de saúde do código aberto",
    view: "Ver achados",
  },
  status: {
    open: "Aberto",
    accepted: "Aceito",
    false_positive: "Falso positivo",
    resolved: "Resolvido",
  },
  severity: {
    high: "Alta",
    medium: "Média",
    low: "Baixa",
  },
  category: {
    security: "Segurança",
    configuration: "Configuração",
    logging: "Logging",
    error_handling: "Tratamento de erros",
    observability: "Observabilidade",
    reliability: "Confiabilidade",
    quality: "Qualidade do código",
  },
  empty: {
    noScanTitle: "Ainda não verificado",
    noScanDescription: "Os achados aparecem após a primeira varredura do repositório.",
    cleanTitle: "Nenhum problema encontrado",
    cleanDescription: "A última varredura não encontrou nada para corrigir aqui.",
    filtered: "Nenhum achado corresponde a este filtro.",
  },
  verdict: {
    acceptTitle: "Aceitar o risco",
    acceptDescription: "O achado é fechado e continua fechado nas próximas varreduras, mesmo que o código ainda o tenha.",
    falsePositiveTitle: "Marcar como falso positivo",
    falsePositiveDescription: "Diga à varredura que ela está errada aqui. O achado continua fechado nas próximas varreduras.",
    reasonLabel: "Motivo (opcional)",
    reasonPlaceholder: "Por que aqui está tudo bem",
    save: "Salvar",
  },
  task: {
    title: "Criar uma tarefa",
    description: "Uma tarefa do quadro cobrindo {count} achados. O agente recebe cada local e como corrigir cada um.",
    titleLabel: "Título",
    notesLabel: "Notas para o agente (opcional)",
    notesPlaceholder: "O que o agente precisa saber primeiro",
    submit: "Criar tarefa",
    created: "Tarefa {key} criada",
    failed: "Não foi possível criar a tarefa",
    defaultTitleMany: "Corrigir {count} achados de saúde do código em {component}",
    severalComponents: "vários componentes",
  },
  rules: {
    hardcoded_secret: {
      title: "Segredo no código",
      description: "Uma chave ou token em formato conhecido está no repositório. Revogue-o e leia-o da configuração.",
    },
    hardcoded_credential: {
      title: "Credencial fixa no código",
      description: "Uma senha, chave ou token está como literal em vez de ser lida da configuração.",
    },
    committed_env_file: {
      title: "Arquivo env com segredos não ignorado",
      description: "Um arquivo .env com segredos reais está versionado ou não é ignorado pelo git.",
    },
    tls_verification_disabled: {
      title: "Verificação TLS desativada",
      description: "A checagem de certificado ou de host está desligada e o tráfego pode ser interceptado.",
    },
    cleartext_traffic_allowed: {
      title: "HTTP sem criptografia permitido",
      description: "O app permite HTTP sem criptografia para qualquer host.",
    },
    sql_injection_risk: {
      title: "Possível injeção de SQL",
      description: "Um valor é inserido numa string SQL em vez de passado como parâmetro.",
    },
    dynamic_code_execution: {
      title: "Execução dinâmica de código",
      description: "eval, exec ou desserialização insegura executam o que a entrada mandar.",
    },
    unsafe_html_injection: {
      title: "HTML sem escape",
      description: "HTML bruto é renderizado sem sanitização, o que permite cross-site scripting.",
    },
    permissive_cors: {
      title: "CORS aceita qualquer origem",
      description: "Qualquer site pode chamar esta API a partir do navegador de um visitante.",
    },
    insecure_token_storage: {
      title: "Tokens em armazenamento simples",
      description: "Tokens ou senhas ficam em localStorage, UserDefaults ou similar.",
    },
    http_body_logging: {
      title: "Corpos ou cabeçalhos registrados no log",
      description: "Corpos e cabeçalhos levam tokens e dados pessoais para os logs.",
    },
    error_details_exposed: {
      title: "Detalhes de erro enviados ao cliente",
      description: "Stack traces ou páginas de erro de desenvolvimento chegam aos usuários.",
    },
    public_source_maps: {
      title: "Source maps públicos",
      description: "O build de produção publica o código-fonte original.",
    },
    debug_mode_in_production: {
      title: "Modo debug em produção",
      description: "A configuração de produção liga o modo debug ou de desenvolvimento.",
    },
    debug_log_level_in_production: {
      title: "Nível de log debug em produção",
      description: "A configuração de produção registra logs em nível debug ou trace.",
    },
    debuggable_release_build: {
      title: "Build de release depurável",
      description: "Qualquer pessoa pode anexar um depurador ao build de release Android.",
    },
    release_minification_disabled: {
      title: "Build de release sem minificação",
      description: "R8/ProGuard está desligado no build de release.",
    },
    unconditional_debug_logging: {
      title: "Logger fixo em debug",
      description: "O nível de log está fixo em debug no código e ignora o ambiente.",
    },
    console_logs_in_production: {
      title: "Saída de console de debug em produção",
      description: "Chamadas console.log/debug/info vão para o bundle e o build não as remove.",
    },
    mobile_debug_prints: {
      title: "Prints de debug em builds de release",
      description: "print, NSLog ou Log.d rodam fora de proteções de debug.",
    },
    print_instead_of_logger: {
      title: "Prints em vez de logs",
      description: "O código do serviço escreve no stdout em vez de usar o logger.",
    },
    print_stack_trace: {
      title: "Stack traces impressos, não registrados",
      description: "printStackTrace ou print_exc ignoram o sistema de logs.",
    },
    no_structured_logging: {
      title: "Sem biblioteca de logging",
      description: "O serviço não tem logger, então não há logs estruturados por nível.",
    },
    no_request_logging: {
      title: "Sem log de requisições",
      description: "O serviço HTTP não registra as requisições (método, status, duração).",
    },
    catch_without_logging: {
      title: "Erros engolidos sem log",
      description: "Erros capturados não são registrados, relançados nem reportados.",
    },
    empty_catch_block: {
      title: "Tratador de erro vazio",
      description: "Um bloco catch, except ou err vazio esconde qualquer falha nesse caminho.",
    },
    bare_except: {
      title: "except sem tipo",
      description: "except: também captura KeyboardInterrupt e SystemExit.",
    },
    no_error_boundary: {
      title: "Sem error boundary do React",
      description: "Um único erro de renderização apaga toda a interface e nada é reportado.",
    },
    no_crash_reporting: {
      title: "Sem relatório de falhas ou erros",
      description: "Erros que os usuários encontram em produção ficam invisíveis.",
    },
    no_health_endpoint: {
      title: "Sem endpoint de health",
      description: "Probes e verificações de deploy não sabem se o serviço está no ar.",
    },
    http_client_without_timeout: {
      title: "Chamadas HTTP sem timeout",
      description: "Uma dependência lenta pode travar requisições indefinidamente.",
    },
    force_unwrap: {
      title: "Muitos force unwraps",
      description: "try!, as! e !! quebram com um nil inesperado.",
    },
    mutable_default_argument: {
      title: "Argumento padrão mutável",
      description: "Um padrão list ou dict é compartilhado entre chamadas.",
    },
    debug_leftovers: {
      title: "Comandos de debug esquecidos",
      description: "debugger, breakpoints ou var_dump/dd ficaram no código.",
    },
    oversized_source_file: {
      title: "Arquivos-fonte muito grandes",
      description: "Arquivos com mais de 1.500 linhas são difíceis de revisar e alterar.",
    },
    typescript_any_overuse: {
      title: "Muitos any / @ts-ignore",
      description: "A checagem de tipos está desligada em muitos lugares.",
    },
    tech_debt_markers: {
      title: "Muitos TODO/FIXME",
      description: "Problemas conhecidos que não são acompanhados em lugar nenhum.",
    },
    no_automated_tests: {
      title: "Sem testes automatizados",
      description: "O componente tem código-fonte mas nenhum arquivo de teste.",
    },
    no_linter_configured: {
      title: "Sem linter configurado",
      description: "Desvios de estilo e padrões comuns de bug não são verificados.",
    },
    wildcard_import: {
      title: "Imports com curinga",
      description: "from x import * esconde de onde vêm os nomes.",
    },
  },
};
