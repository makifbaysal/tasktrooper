import type { TelemetryDict, TelemetryKindsDict } from "@/locales/en/opsTelemetry";

// Portuguese strings for the console's telemetry views; mirrors en/opsTelemetry.ts key for key.
export const telemetry: TelemetryDict = {
  table: {
    readAt: "Lida {time}",
    readAtTitle: "Lida em {time}. O servidor guarda uma leitura por 30 segundos, então ela pode ser até 30 s mais antiga que a última atualização desta página.",
    window: "Janela de {span}",
    windowUnknown: "Cobre a janela recente da própria fonte",
    windowDocs: "Janelas das tabelas",
    rows: "{count} linhas",
    flag: "Marcação",
    sortBy: "Ordenar por {column}",
    truncated: "A fonte tem mais linhas do que enviou: as primeiras {count} são mostradas.",
    showAll: "Mostrar todas as {count} linhas",
    noRows: "A fonte não tem nada para isso agora.",
    noValue: "Sem valor",
    unnamedSource: "Fonte de dados removida",
    recipe: "Receita {recipe}",
    customQuery: "Consulta própria",
    loadFailed: "Não foi possível ler esta tabela",
    refreshFailed: "Não foi possível atualizar, então a última resposta é mostrada",
    errors: {
      failed: "Não foi possível ler esta tabela",
      needsParams: "Falta um parâmetro no vínculo",
      needsParamsBody: "A tabela precisa de um valor que o vínculo ainda não tem.",
      notFound: "A fonte de dados não existe mais",
      notFoundBody: "A fonte de dados de onde esta tabela era lida não existe mais. Vincule outra.",
      unsupported: "Esta fonte não lista linhas",
      unsupportedBody: "O tipo dela lê o sinal ao longo do tempo, mas não uma linha por item. Uma receita com esta tabela oferece isso.",
      badQuery: "A fonte recusou a consulta da tabela",
      timeout: "A fonte não respondeu a tempo",
      timeoutBody: "Ela pode estar ocupada. Tente de novo daqui a pouco.",
      upstream: "A fonte não respondeu",
      openSignals: "Abrir Sinais",
      retry: "Tentar de novo",
    },
  },
  endpoints: {
    title: "Endpoints",
    hint: "Cada tabela cobre a janela recente da sua fonte, lida a cada minuto",
    scopeNote: "Cada tabela de endpoints cobre a janela recente da sua fonte, indicada na tabela, então o intervalo de tempo não se aplica.",
    flags: {
      errors: "Maior taxa de erros",
      slowest: "p95 mais lento",
    },
    empty: {
      title: "Ainda não há tabela de endpoints",
      body: "Os endpoints de {name} (vazão, taxa de erros e p95 por rota) vêm de uma fonte cuja receita os lista: métricas OpenTelemetry no Prometheus, no New Relic ou no Datadog. Vincule um dos sinais dela em Sinais.",
      signals: "Abrir Sinais",
      addSource: "Adicionar uma fonte de dados",
    },
  },
  pages: {
    scopeNote: "Cada tabela web cobre a janela recente da sua fonte, indicada na tabela, então o intervalo de tempo não se aplica.",
    hint: "Cada tabela cobre a janela recente da sua fonte de análise web",
    tabsLabel: "Tabelas web",
    tabs: {
      pages: "Páginas",
      referrers: "Referenciadores",
      countries: "Países",
      devices: "Dispositivos",
    },
    empty: {
      title: "Ainda não há uma tabela {table}",
      body: "A tabela {table} de {name} vem de uma fonte de análise web (PostHog, Google Analytics 4 ou Plausible) vinculada em Sinais.",
      signals: "Abrir Sinais",
      addSource: "Adicionar uma fonte de dados",
    },
  },
  crashes: {
    scopeNote: "Cada tabela de falhas cobre a janela recente da sua fonte, indicada na tabela, então o intervalo de tempo não se aplica.",
    openIssue: "Abrir no console de origem",
    issues: {
      title: "Problemas de falha e ANR",
      hint: "Os problemas de erro do Google Play e as issues do Sentry, primeiro os que atingem mais usuários",
    },
    signatures: {
      title: "Assinaturas de diagnóstico",
      hint: "As assinaturas de travamento, inicialização e gravação em disco do build mais recente no App Store Connect",
    },
    types: {
      crash: "Falha",
      anr: "ANR",
      non_fatal: "Não fatal",
      hangs: "Travamento",
      launches: "Inicialização",
      disk_writes: "Gravações em disco",
      fatal: "Fatal",
      error: "Erro",
      warning: "Aviso",
    },
    empty: {
      title: "Ainda não há dados de falhas",
      body: "As falhas de {name} vêm do Google Play vitals, do App Store Connect ou do Sentry. Veja as sugestões em Sinais: um app de loja vinculado oferece a fonte dele em um passo.",
      signals: "Abrir Sinais",
      addSource: "Adicionar uma fonte de dados",
    },
  },
  traces: {
    minDuration: "Duração mínima (ms)",
    minDurationHint: "Só traces com pelo menos essa duração. Pressione Enter ou saia do campo para buscar.",
    manage: "Gerenciar o vínculo de traces",
    loadFailed: "Não foi possível buscar os traces",
    refreshFailed: "Não foi possível atualizar os traces, então a última resposta é mostrada",
    capped: "Os {count} traces mais recentes são mostrados. Restrinja o intervalo ou aumente a duração mínima para ver o resto.",
    openFromLog: "Abrir trace",
    clamped: {
      title: "Buscando nos últimos 7 dias do intervalo",
      body: "Uma busca de traces cobre no máximo 7 dias, então ela começa em {since}.",
    },
    unbound: {
      title: "Conecte uma fonte de traces",
      body: "Os traces de {name} vêm de um backend de tracing, como o Grafana Tempo. Vincule os traces dele a uma fonte Tempo ou adicione uma antes.",
      bind: "Vincular traces",
      addSource: "Adicionar uma fonte de dados",
    },
    empty: {
      title: "Nenhum trace neste intervalo",
      body: "A fonte de traces não tem nenhum trace deste componente desde {since}.",
      withMin: "Nenhum trace neste intervalo levou {min} ms ou mais.",
      clearMin: "Limpar a duração mínima",
    },
    list: {
      label: "Traces",
      count: "{count} traces",
      failedCount: "{count} com erros",
      order: "Ordem",
      orders: {
        newest: "Mais recentes primeiro",
        slowest: "Mais lentos primeiro",
      },
      columns: {
        start: "Início",
        root: "Span raiz",
        duration: "Duração",
        spans: "Spans",
        status: "Status",
      },
      open: "Abrir o trace {name}",
      noRoot: "Span raiz não encontrado",
      noService: "Serviço desconhecido",
      error: "Erro",
      ok: "OK",
    },
    detail: {
      back: "Voltar para os traces",
      untitled: "Trace",
      copy: "Copiar o ID do trace",
      copied: "ID do trace copiado",
      duration: "Duração",
      spans: "Spans",
      errorCount: "Erros",
      services: "Serviços",
      serviceSpans: "{count} spans",
      serviceErrors: "{count} com falha",
      viewLabel: "Visualização do trace",
      views: {
        waterfall: "Cascata",
        table: "Tabela",
      },
      hint: "Escolha um span para ver os atributos dele.",
      noSpans: "A fonte enviou este trace sem spans.",
      refreshFailed: "Não foi possível ler o trace de novo, então a última resposta é mostrada",
      truncated: {
        title: "Nem todos os spans são mostrados",
        body: "O trace tem mais spans do que cabem em uma resposta; os primeiros {count} são mostrados.",
      },
      elsewhere: {
        title: "Encontrado por outro componente",
        body: "Este componente não tem vínculo de traces, então o trace foi procurado em todas as fontes de traces do ambiente e encontrado por {name}.",
        action: "Abrir em {name}",
      },
      errors: {
        notFound: "Trace não encontrado",
        notFoundBody: "Nenhuma fonte de traces deste ambiente tem o trace {id}. Ele pode ser mais antigo do que a fonte guarda.",
        unbound: "Nenhuma fonte de traces onde procurar",
        unboundBody: "Nenhum componente deste ambiente tem vínculo de traces, então não é possível procurar o trace {id}.",
        timeout: "A fonte de traces não respondeu a tempo",
        timeoutBody: "Ela pode estar ocupada. Tente de novo daqui a pouco.",
        failed: "Não foi possível ler o trace",
      },
    },
    waterfall: {
      span: "Span",
      listLabel: "Spans",
      tableLabel: "Spans em tabela",
      rowLabel: "{service} {name}, começa em +{offset}, leva {duration}",
      failed: "com falha",
      noService: "Serviço desconhecido",
      noName: "Span sem nome",
      expand: "Mostrar os spans sob {name}",
      collapse: "Ocultar os spans sob {name}",
      columns: {
        depth: "Profundidade",
        service: "Serviço",
        operation: "Operação",
        start: "Começa em",
        duration: "Duração",
        status: "Status",
      },
      status: {
        unset: "Não definido",
        ok: "OK",
        error: "Erro",
      },
    },
    span: {
      close: "Fechar o span",
      duration: "Duração",
      offset: "Começa em",
      started: "Início",
      id: "ID do span",
      parent: "Span pai",
      copy: "Copiar o ID do span",
      copied: "ID do span copiado",
      attributes: "Atributos",
      resource: "Recurso",
      noAttributes: "Sem atributos.",
      statuses: {
        ok: "OK",
        error: "Erro",
      },
      kinds: {
        server: "Servidor",
        client: "Cliente",
        internal: "Interno",
        producer: "Produtor",
        consumer: "Consumidor",
      },
    },
  },
  webVitals: {
    title: "Core Web Vitals",
    hint: "O p75 mais recente dos carregamentos de página no intervalo, comparado com os limites do Google",
    traffic: "Tráfego",
    bands: {
      good: "Bom",
      needsImprovement: "Precisa de melhorias",
      poor: "Ruim",
    },
    quantileP75: "p75",
    thresholds: "Bom até {good}, ruim acima de {poor}",
    meterLabel: "{band}: bom até {good}, ruim acima de {poor}",
    bind: "Vincular uma fonte de dados",
    noData: "Sem dados neste período",
    readFailed: "Não foi possível ler",
    latest: "Último valor do intervalo",
    live: "Valor ao vivo, sem histórico",
  },
  mobileVitals: {
    window: "Últimos 7 dias em {env}, seja qual for o intervalo.",
    asOf: "Dados até {date}",
    bind: "Vincular em Sinais",
    noData: "Sem dados nos últimos 7 dias",
    notYet: "Ainda não publicado",
    playLag: "Os dados do Google Play chegam com 1–2 dias de atraso, então os dias mais recentes são preenchidos depois.",
    readFailed: "Não foi possível ler",
  },
  offers: {
    title: "Fontes de dados para conectar",
    found: "{count} fontes de dados para conectar",
    bound: "{count} sinais vinculados por {name}",
    titles: {
      playVitals: "Conectar o Google Play vitals deste app",
      appStoreMetrics: "Conectar as métricas da App Store deste app",
      generic: "Conectar {kind} para este app",
    },
    bodies: {
      playVitals: "Usuários sem falhas, taxa de falhas, taxa de ANR e os principais problemas de falha, lidos com a credencial do Google Play do espaço de trabalho. Nada para instalar.",
      appStoreMetrics: "Tempo de inicialização por versão do app e as assinaturas de travamento, inicialização e gravação em disco de cada build, lidos com a chave do App Store Connect do espaço de trabalho. Nada para instalar.",
      generic: "Uma fonte {kind} leria estes sinais deste app.",
    },
    credentialMissing: {
      google_play: "Salve primeiro a credencial do Google Play.",
      asc: "Salve primeiro a chave do App Store Connect.",
    },
    saveCredential: "Configurações → Integrações",
    connect: "Conectar",
    connecting: "Conectando…",
    notNow: "Agora não",
    recipe: "Receita {recipe}",
  },
  binding: {
    noPreviewTitle: "Sem prévia para este sinal",
    noPreview: "{signal} não é uma série para gráfico: logs, traces e grupos de erros são lidos no explorador de logs, em Traces e na Caixa de erros. Salve para vinculá-lo.",
  },
  errors: {
    providers: {
      sentry: "Sentry",
    },
    fromProvider: "Lido do {provider}",
    noTask: "Este grupo não indica nenhuma fonte de dados para criar uma tarefa. Abra-o em {provider}.",
    openSource: "Fonte de dados {provider}",
    fixSource: "Abrir a fonte de dados",
  },
  sources: {
    storeCredentials: {
      google_play: "Conta de serviço do Google Play",
      asc: "Chave de API do App Store Connect",
    },
    storeCredentialWorkspace: "salva para o espaço de trabalho",
    storeCredentialPlaceholder: "Escolha a {credential}",
    noStoreCredential: "Nenhuma {credential} foi salva ainda.",
    saveStoreCredential: "Salve-a em Configurações → Integrações",
    namePlaceholderStore: "Se ficar vazio, usa o nome do tipo",
    errors: {
      store_credential: {
        not_found: "Essa credencial ainda não foi salva. Salve-a antes em Configurações → Integrações.",
        wrong_provider: "Essa credencial é da outra loja.",
        not_permitted: "Nesta instalação não é possível usar credenciais de loja em fontes de dados.",
      },
      org: {
        invalid: "Informe o slug da organização, como em sentry.io/organizations/<slug>/.",
      },
      index: {
        invalid: "Informe um índice, data stream ou padrão, como logs-*; vários separados por vírgula.",
      },
    },
  },
};

const AUTH_OPTIONS = {
  none: "Nenhuma",
  bearer: "Token bearer",
  basic: "Basic (usuário e senha)",
};

export const telemetryKinds: TelemetryKindsDict = {
  google_play_vitals: {
    label: "Google Play vitals",
    description:
      "Os Android vitals do Google Play (taxas de falha e de ANR, inicializações lentas e os principais problemas de falha), lidos com a credencial do Google Play do espaço de trabalho. Nada para instalar; o Play os publica com um ou dois dias de atraso.",
    queryPlaceholder: 'ex.: {"metric_set": "crashRateMetricSet", "metric": "userPerceivedCrashRate", "as": "percent"}',
    queryHint:
      'JSON: {"package"?, "metric_set", "metric", "dimension"?, "filter"?, "period"?, "as"?}. metric_set é crashRateMetricSet, anrRateMetricSet, slowStartRateMetricSet ou errorCountMetricSet; period é daily (padrão) ou hourly; as é value, percent ou free_percent. Os dados diários chegam com 1–2 dias de atraso.',
    fields: {
      store_credential: {
        label: "Credencial do Google Play",
        help: "A conta de serviço salva em Configurações → Integrações → Google Play. Ela precisa da Play Developer Reporting API ativada no projeto do Google Cloud dela e, no Play Console, de permissão para ver as informações do app.",
      },
      package_name: {
        label: "Nome do pacote",
        help: "O app lido quando a consulta não indica nenhum. O vínculo de um componente o preenche com o app Android vinculado ao repositório dele.",
        placeholder: "com.example.app",
      },
    },
  },
  app_store_metrics: {
    label: "Métricas do App Store Connect",
    description:
      "O tempo de inicialização, os travamentos, a memória, as gravações em disco e o uso de bateria de um app iOS como o Xcode Organizer os mostra, por versão do app, e os diagnósticos de travamento e inicialização de cada build, lidos com a chave do App Store Connect do espaço de trabalho. A Apple não informa taxa sem falhas.",
    queryPlaceholder: 'ex.: {"metric": "launchTime", "percentile": "p90"}',
    queryHint:
      'JSON: {"app"?, "metric", "percentile"?, "device"?, "version"?}. metric é uma métrica do Xcode Organizer (launchTime, hangRate, …) ou uma categoria inteira (launch, hang, memory, disk, battery); percentile é p50 ou p90. Os valores vêm por versão do app, desenhados em degraus.',
    fields: {
      store_credential: {
        label: "Credencial do App Store Connect",
        help: "A chave de API salva em Configurações → Integrações → App Store Connect.",
      },
      app_id: {
        label: "ID do app",
        help: "O app lido quando a consulta não indica nenhum: o ID dele no App Store Connect. O vínculo de um componente o preenche com o app iOS vinculado ao repositório dele.",
        placeholder: "1234567890",
      },
    },
  },
  sentry: {
    label: "Sentry",
    description:
      "Usuários e sessões sem falhas, contagens de erros e issues do Sentry, para projetos mobile, web e backend. As issues dele entram na Caixa de erros.",
    queryPlaceholder: 'ex.: {"project": "ios-app", "metric": "crash_free_users"}',
    queryHint:
      'JSON: {"project", "metric"?, "environment"?, "release"?, "search"?, "days"?}. metric é events (padrão), event_rate, crash_free_users, crash_free_sessions, crash_rate, crash_rate_users, sessions ou users; as métricas de falha precisam do rastreamento de sessões ativado no SDK.',
    fields: {
      url: {
        label: "URL do Sentry",
        help: "https://sentry.io para a nuvem do Sentry (https://de.sentry.io para uma organização na região da UE) ou o endereço do seu Sentry auto-hospedado.",
        placeholder: "https://sentry.io",
      },
      org: {
        label: "Organização",
        help: "O slug da organização, como em sentry.io/organizations/<slug>/.",
        placeholder: "acme",
      },
      token: {
        label: "Token de autenticação",
        help: "Um token com os escopos org:read, project:read e event:read: um token de organização ou o de uma integração interna. Guardado com criptografia.",
      },
    },
  },
  loki: {
    label: "Loki",
    description: "Grafana Loki ou Grafana Cloud Logs: as linhas de log de um componente e as taxas de erro contadas a partir delas com LogQL.",
    queryPlaceholder: 'ex.: {service_name="api"} |= "error"',
    queryHint:
      'LogQL. Uma consulta de logs ({service_name="api"} |= "timeout") alimenta o explorador de logs; uma consulta de métrica (sum(rate({service_name="api"} | detected_level="error" [5m]))) desenha um gráfico.',
    fields: {
      url: {
        help: "A URL base do Loki, sem /loki/api/v1. No Grafana Cloud, a URL de Logs (https://logs-prod-<n>.grafana.net).",
        placeholder: "http://localhost:3100",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "No Grafana Cloud, o ID de usuário de Logs.",
      },
      password: {
        help: "No Grafana Cloud, um token de access policy com logs:read.",
      },
    },
  },
  elasticsearch: {
    label: "Elasticsearch / OpenSearch",
    description: "Linhas de log guardadas no Elasticsearch, no OpenSearch ou no Elastic Cloud: um padrão de índice, filtrado por campo.",
    queryPlaceholder: 'ex.: {"filters": {"service.name": "billing"}}',
    queryHint: 'JSON: {"filters": {"<field>": "<value>", …}}; todos os filtros precisam corresponder, no máximo 32. Exato em um campo keyword; em um campo text, todas as palavras.',
    fields: {
      url: {
        help: "A URL base do Elasticsearch ou do OpenSearch. No Elastic Cloud, o endpoint do Elasticsearch da implantação.",
        placeholder: "http://localhost:9200",
      },
      index: {
        label: "Índice",
        help: "O índice, data stream ou padrão que guarda os logs; vários separados por vírgula (logs-*,app-*).",
        placeholder: "logs-*",
      },
      time_field: {
        label: "Campo de data e hora",
        help: "O campo de data pelo qual cada linha é ordenada e filtrada. Se ficar vazio, @timestamp.",
        placeholder: "@timestamp",
      },
      message_field: {
        label: "Campo da mensagem",
        help: "O campo com a própria linha; o filtro de texto busca nele. Se ficar vazio, message.",
        placeholder: "message",
      },
      level_field: {
        label: "Campo do nível",
        help: "O campo com o nível (log.level no ECS). Se ficar vazio, log.level.",
        placeholder: "log.level",
      },
      auth: {
        options: {
          none: "Nenhuma",
          basic: "Basic (usuário e senha)",
          api_key: "Chave de API",
        },
      },
      api_key: {
        label: "Chave de API",
        help: "A chave de API codificada (base64 de id:key), como o Kibana a mostra quando a chave é criada. Guardada com criptografia.",
      },
    },
  },
  tempo: {
    label: "Tempo",
    description: "Grafana Tempo ou Grafana Cloud Traces: os traces de um componente, buscados com TraceQL.",
    queryPlaceholder: 'ex.: { resource.service.name = "api" }',
    queryHint: 'TraceQL: os spans de onde partem os traces de um componente, como { resource.service.name = "api" }. Lido em Traces, nunca como gráfico.',
    fields: {
      url: {
        help: "A base da API HTTP do Tempo, sem /api. No Grafana Cloud, a URL de Traces terminada em /tempo.",
        placeholder: "http://localhost:3200",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "No Grafana Cloud, o ID de usuário de Traces.",
      },
      password: {
        help: "No Grafana Cloud, um token de access policy com traces:read.",
      },
    },
  },
  posthog: {
    label: "PostHog",
    description:
      "Visualizações de página, sessões, visitantes, exceções de JavaScript e web vitals do PostHog, com as principais páginas, referenciadores, países e dispositivos. Nada para instalar além do posthog-js.",
    queryPlaceholder: 'ex.: {"metric": "lcp", "quantile": 0.75}',
    queryHint:
      'JSON: {"metric"?, "event"?, "quantile"?, "host"?}. metric é page_views (padrão), sessions, visitors, js_errors, events (com o event que ele conta) ou um web vital: lcp, inp, cls, fcp, no quantil 0.75 se nenhum for informado. host restringe a um único hostname.',
    fields: {
      url: {
        label: "URL do PostHog",
        help: "https://us.posthog.com ou https://eu.posthog.com para o PostHog Cloud, ou o endereço do seu PostHog auto-hospedado.",
        placeholder: "https://us.posthog.com",
      },
      project_id: {
        label: "ID do projeto",
        help: "O número na URL do projeto (/project/<id>) ou em Project settings.",
        placeholder: "12345",
      },
      personal_api_key: {
        label: "Chave de API pessoal",
        help: "Uma chave de API pessoal (phx_…) com o escopo Query Read neste projeto. Guardada com criptografia.",
      },
    },
  },
  ga4: {
    label: "Google Analytics 4",
    description:
      "Visualizações de página, sessões e usuários ativos de uma propriedade do Google Analytics 4, com as principais páginas, referenciadores, países e dispositivos. O GA4 leva até dois dias para processar os dados, então os pontos mais recentes ainda podem subir.",
    queryPlaceholder: 'ex.: {"metric": "page_views"}',
    queryHint:
      'JSON: {"metric"?, "event"?, "host"?, "property"?}. metric é page_views (padrão), sessions, visitors ou events (com o event que ele conta, o eventName do GA4). property lê outra propriedade que não a da fonte.',
    fields: {
      property_id: {
        label: "ID da propriedade",
        help: "O número em Administrador → Detalhes da propriedade (não o ID da métrica G-…).",
        placeholder: "123456789",
      },
      auth: {
        label: "Entrar com",
        help: "Uma conta Google Cloud conectada, ou uma chave de conta de serviço só para esta fonte.",
        options: {
          cloud_account: "Uma conta Google Cloud conectada",
          service_account: "Uma chave de conta de serviço",
        },
      },
      cloud_account_id: {
        label: "Conta Google Cloud",
        help: "A conta de serviço dela precisa ser Leitor da propriedade (Administrador → Gerenciamento de acesso à propriedade), com a Google Analytics Data API ativada no projeto dela.",
      },
      service_account_json: {
        label: "Chave da conta de serviço (JSON)",
        help: "Uma chave de uma conta de serviço que seja Leitor da propriedade, com a Google Analytics Data API ativada no projeto dela. Cole o arquivo JSON inteiro. Guardada com criptografia.",
      },
    },
  },
  plausible: {
    label: "Plausible",
    description: "Visualizações de página, visitas e visitantes do Plausible Analytics, com as principais páginas, origens, países e dispositivos.",
    queryPlaceholder: 'ex.: {"metric": "visitors"}',
    queryHint: 'JSON: {"metric"?, "host"?, "site"?}. metric é page_views (padrão), sessions (visitas) ou visitors. site lê outro site que não o da fonte.',
    fields: {
      url: {
        label: "URL do Plausible",
        help: "https://plausible.io ou o endereço do seu Plausible auto-hospedado.",
        placeholder: "https://plausible.io",
      },
      site_id: {
        label: "Site",
        help: "O domínio do site como ele está nomeado no Plausible.",
        placeholder: "example.com",
      },
      api_key: {
        label: "Chave da Stats API",
        help: "Uma chave da Stats API (Account settings → API keys) da equipe dona do site. Guardada com criptografia.",
      },
    },
  },
  datadog: {
    label: "Datadog",
    description: "Taxas de requisições, erros e latência do APM, e métricas de hosts ou contêineres, pela API de métricas do Datadog.",
    queryPlaceholder: "ex.: sum:trace.http.request.hits{service:api,env:prod}.as_rate()",
    queryHint:
      "Uma consulta de métricas do Datadog, como um dashboard a escreve: avg:system.cpu.user{service:web} by {host}. Uma tabela lê o valor mais recente de cada grupo de tags.",
    fields: {
      site: {
        options: {
          datadoghq_com: "US1 · datadoghq.com",
          us3_datadoghq_com: "US3 · us3.datadoghq.com",
          us5_datadoghq_com: "US5 · us5.datadoghq.com",
          datadoghq_eu: "EU1 · datadoghq.eu",
          ap1_datadoghq_com: "AP1 · ap1.datadoghq.com",
          "ddog-gov_com": "US1-FED · ddog-gov.com",
        },
        label: "Site",
        help: "O site na sua URL do Datadog: app.datadoghq.com é datadoghq.com, app.datadoghq.eu é datadoghq.eu, us5.datadoghq.com é us5.datadoghq.com.",
      },
      api_key: {
        label: "Chave de API",
        help: "Organization settings → API keys. Guardada com criptografia.",
      },
      app_key: {
        label: "Chave de aplicação",
        help: "Organization settings → Application keys; uma chave com escopo restrito precisa de timeseries_query e metrics_read. Guardada com criptografia.",
      },
    },
  },
  newrelic: {
    label: "New Relic",
    description: "Vazão, erros e latência do APM, visualizações de página e web vitals do browser, e taxas de falha mobile, do New Relic com NRQL.",
    queryPlaceholder: "ex.: SELECT percentile(duration * 1000, 95) FROM Transaction WHERE appName = 'api'",
    queryHint:
      "NRQL, sem SINCE, UNTIL ou TIMESERIES: o tipo os acrescenta para o intervalo. Uma tabela é uma consulta FACET, uma linha por facet.",
    fields: {
      region: {
        label: "Região",
        help: "Onde ficam os dados da conta: eu para one.eu.newrelic.com; caso contrário, us.",
        options: {
          us: "US",
          eu: "EU",
        },
      },
      account_id: {
        label: "ID da conta",
        help: "O número da conta, mostrado sob o nome dela no seletor de contas.",
        placeholder: "1234567",
      },
      api_key: {
        label: "Chave de usuário",
        help: "Uma chave de usuário (NRAK-…) de API keys, de um usuário que possa consultar a conta. Guardada com criptografia.",
      },
    },
  },
};
