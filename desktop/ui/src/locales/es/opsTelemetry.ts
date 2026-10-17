import type { TelemetryDict, TelemetryKindsDict } from "@/locales/en/opsTelemetry";

// Spanish strings for the console's telemetry views; mirrors en/opsTelemetry.ts key for key.
export const telemetry: TelemetryDict = {
  table: {
    readAt: "Leída {time}",
    readAtTitle: "Leída el {time}. El servidor guarda una lectura 30 segundos, así que puede ser hasta 30 s más antigua que la última actualización de esta página.",
    window: "Ventana de {span}",
    windowUnknown: "Cubre la ventana reciente propia de la fuente",
    windowDocs: "Ventanas de las tablas",
    rows: "{count} filas",
    flag: "Marca",
    sortBy: "Ordenar por {column}",
    truncated: "La fuente tiene más filas de las que envió: se muestran las primeras {count}.",
    showAll: "Mostrar las {count} filas",
    noRows: "La fuente no tiene nada para esto ahora mismo.",
    noValue: "Sin valor",
    unnamedSource: "Fuente de datos eliminada",
    recipe: "Receta {recipe}",
    customQuery: "Consulta propia",
    loadFailed: "No se pudo leer esta tabla",
    refreshFailed: "No se pudo actualizar, así que se muestra la última respuesta",
    errors: {
      failed: "No se pudo leer esta tabla",
      needsParams: "A la vinculación le falta un parámetro",
      needsParamsBody: "La tabla necesita un valor que su vinculación aún no tiene.",
      notFound: "La fuente de datos ya no existe",
      notFoundBody: "La fuente de datos de la que se leía esta tabla ya no existe. Vincula otra.",
      unsupported: "Esta fuente no puede listar filas",
      unsupportedBody: "Su tipo puede leer la señal a lo largo del tiempo, pero no una fila por elemento. Una receta que incluya esta tabla sí la ofrece.",
      badQuery: "La fuente rechazó la consulta de la tabla",
      timeout: "La fuente no respondió a tiempo",
      timeoutBody: "Puede que esté ocupada. Vuelve a intentarlo en un momento.",
      upstream: "La fuente no respondió",
      openSignals: "Abrir Señales",
      retry: "Reintentar",
    },
  },
  endpoints: {
    title: "Endpoints",
    hint: "Cada tabla cubre la ventana reciente de su fuente, leída cada minuto",
    scopeNote: "Cada tabla de endpoints cubre la ventana reciente de su fuente, indicada en la tabla, así que el intervalo de tiempo no se aplica.",
    flags: {
      errors: "Mayor tasa de errores",
      slowest: "p95 más lento",
    },
    empty: {
      title: "Todavía no hay tabla de endpoints",
      body: "Los endpoints de {name} (rendimiento, tasa de errores y p95 por ruta) salen de una fuente cuya receta los lista: métricas de OpenTelemetry en Prometheus, New Relic o Datadog. Vincula una de sus señales en Señales.",
      signals: "Abrir Señales",
      addSource: "Añadir una fuente de datos",
    },
  },
  pages: {
    scopeNote: "Cada tabla web cubre la ventana reciente de su fuente, indicada en la tabla, así que el intervalo de tiempo no se aplica.",
    hint: "Cada tabla cubre la ventana reciente de su fuente de analítica web",
    tabsLabel: "Tablas web",
    tabs: {
      pages: "Páginas",
      referrers: "Referentes",
      countries: "Países",
      devices: "Dispositivos",
    },
    empty: {
      title: "Todavía no hay tabla de {table}",
      body: "En {name}, la tabla de {table} sale de una fuente de analítica web (PostHog, Google Analytics 4 o Plausible) vinculada en Señales.",
      signals: "Abrir Señales",
      addSource: "Añadir una fuente de datos",
    },
  },
  crashes: {
    scopeNote: "Cada tabla de fallos cubre la ventana reciente de su fuente, indicada en la tabla, así que el intervalo de tiempo no se aplica.",
    openIssue: "Abrir en su consola",
    issues: {
      title: "Grupos de fallos y ANR",
      hint: "Los grupos de errores de Google Play y los de Sentry, primero los que afectan a más usuarios",
    },
    signatures: {
      title: "Firmas de diagnóstico",
      hint: "Las firmas de bloqueos, arranques y escrituras en disco de la compilación más reciente, según App Store Connect",
    },
    types: {
      crash: "Fallo",
      anr: "ANR",
      non_fatal: "No fatal",
      hangs: "Bloqueo",
      launches: "Arranque",
      disk_writes: "Escrituras en disco",
      fatal: "Fatal",
      error: "Error",
      warning: "Advertencia",
    },
    empty: {
      title: "Todavía no hay datos de fallos",
      body: "Los fallos de {name} vienen de Google Play vitals, App Store Connect o Sentry. Encontrarás sugerencias en Señales: una app de tienda vinculada ofrece su fuente en un solo paso.",
      signals: "Abrir Señales",
      addSource: "Añadir una fuente de datos",
    },
  },
  traces: {
    minDuration: "Duración mínima (ms)",
    minDurationHint: "Solo las trazas que duran al menos esto. Pulsa Intro o sal del campo para buscar.",
    manage: "Gestionar la vinculación de trazas",
    loadFailed: "No se pudieron buscar las trazas",
    refreshFailed: "No se pudieron actualizar las trazas, así que se muestra la última respuesta",
    capped: "Se muestran las {count} trazas más recientes. Acota el intervalo o sube la duración mínima para ver el resto.",
    openFromLog: "Abrir la traza",
    clamped: {
      title: "Se buscan los últimos 7 días del intervalo",
      body: "Una búsqueda de trazas abarca como máximo 7 días, así que empieza en {since}.",
    },
    unbound: {
      title: "Conecta una fuente de trazas",
      body: "Las trazas de {name} vienen de un backend de trazas como Grafana Tempo. Vincula sus trazas a una fuente de Tempo, o añade una antes.",
      bind: "Vincular trazas",
      addSource: "Añadir una fuente de datos",
    },
    empty: {
      title: "No hay trazas en este intervalo",
      body: "La fuente de trazas no tiene ninguna de este componente desde {since}.",
      withMin: "Ninguna traza de este intervalo duró {min} ms o más.",
      clearMin: "Quitar la duración mínima",
    },
    list: {
      label: "Trazas",
      count: "{count} trazas",
      failedCount: "{count} con errores",
      order: "Orden",
      orders: {
        newest: "Las más recientes primero",
        slowest: "Las más lentas primero",
      },
      columns: {
        start: "Inicio",
        root: "Span raíz",
        duration: "Duración",
        spans: "Spans",
        status: "Estado",
      },
      open: "Abrir la traza {name}",
      noRoot: "No se encontró el span raíz",
      noService: "Servicio desconocido",
      error: "Error",
      ok: "OK",
    },
    detail: {
      back: "Volver a las trazas",
      untitled: "Traza",
      copy: "Copiar el ID de traza",
      copied: "ID de traza copiado",
      duration: "Duración",
      spans: "Spans",
      errorCount: "Errores",
      services: "Servicios",
      serviceSpans: "{count} spans",
      serviceErrors: "{count} fallidos",
      viewLabel: "Vista de la traza",
      views: {
        waterfall: "Cascada",
        table: "Tabla",
      },
      hint: "Elige un span para ver sus atributos.",
      noSpans: "La fuente envió esta traza sin spans.",
      refreshFailed: "No se pudo volver a leer la traza, así que se muestra la última respuesta",
      truncated: {
        title: "No se muestran todos los spans",
        body: "La traza tiene más spans de los que caben en una respuesta; se muestran los primeros {count}.",
      },
      elsewhere: {
        title: "Encontrada a través de otro componente",
        body: "Este componente no tiene vinculación de trazas, así que la traza se buscó en todas las fuentes de trazas del entorno y se encontró a través de {name}.",
        action: "Abrirla en {name}",
      },
      errors: {
        notFound: "No se encontró la traza",
        notFoundBody: "Ninguna fuente de trazas de este entorno tiene la traza {id}. Puede que sea más antigua de lo que conserva la fuente.",
        unbound: "No hay ninguna fuente de trazas en la que buscar",
        unboundBody: "Ningún componente de este entorno tiene vinculación de trazas, así que no se puede buscar la traza {id}.",
        timeout: "La fuente de trazas no respondió a tiempo",
        timeoutBody: "Puede que esté ocupada. Vuelve a intentarlo en un momento.",
        failed: "No se pudo leer la traza",
      },
    },
    waterfall: {
      span: "Span",
      listLabel: "Spans",
      tableLabel: "Spans en forma de tabla",
      rowLabel: "{service} {name}, empieza en +{offset}, dura {duration}",
      failed: "fallido",
      noService: "Servicio desconocido",
      noName: "Span sin nombre",
      expand: "Mostrar los spans bajo {name}",
      collapse: "Ocultar los spans bajo {name}",
      columns: {
        depth: "Profundidad",
        service: "Servicio",
        operation: "Operación",
        start: "Empieza en",
        duration: "Duración",
        status: "Estado",
      },
      status: {
        unset: "Sin definir",
        ok: "OK",
        error: "Error",
      },
    },
    span: {
      close: "Cerrar el span",
      duration: "Duración",
      offset: "Empieza en",
      started: "Inicio",
      id: "ID del span",
      parent: "Span padre",
      copy: "Copiar el ID del span",
      copied: "ID del span copiado",
      attributes: "Atributos",
      resource: "Recurso",
      noAttributes: "Sin atributos.",
      statuses: {
        ok: "OK",
        error: "Error",
      },
      kinds: {
        server: "Servidor",
        client: "Cliente",
        internal: "Interno",
        producer: "Productor",
        consumer: "Consumidor",
      },
    },
  },
  webVitals: {
    title: "Core Web Vitals",
    hint: "El p75 más reciente de las cargas de página del intervalo, frente a los umbrales de Google",
    traffic: "Tráfico",
    bands: {
      good: "Bueno",
      needsImprovement: "Necesita mejorar",
      poor: "Deficiente",
    },
    quantileP75: "p75",
    thresholds: "Bueno hasta {good}, deficiente a partir de {poor}",
    meterLabel: "{band}: bueno hasta {good}, deficiente a partir de {poor}",
    bind: "Vincular una fuente de datos",
    noData: "No hay datos en este intervalo",
    readFailed: "No se pudo leer",
    latest: "Último valor del intervalo",
    live: "Valor en vivo, sin historial",
  },
  mobileVitals: {
    window: "Últimos 7 días en {env}, sea cual sea el intervalo.",
    asOf: "A fecha de {date}",
    bind: "Vincular en Señales",
    noData: "No hay datos en los últimos 7 días",
    notYet: "Aún no publicado",
    playLag: "Los datos de Google Play llevan 1–2 días de retraso, así que los días más recientes se completan más tarde.",
    readFailed: "No se pudo leer",
  },
  offers: {
    title: "Fuentes de datos para conectar",
    found: "{count} fuentes de datos para conectar",
    bound: "{count} señales vinculadas a través de {name}",
    titles: {
      playVitals: "Conecta Google Play vitals para esta app",
      appStoreMetrics: "Conecta las métricas de App Store para esta app",
      generic: "Conecta {kind} para esta app",
    },
    bodies: {
      playVitals: "Usuarios sin fallos, tasa de fallos, tasa de ANR y los principales grupos de fallos, leídos con la credencial de Google Play del espacio de trabajo. No hay que instalar nada.",
      appStoreMetrics: "Tiempo de arranque por versión de la app y las firmas de bloqueos, arranques y escrituras en disco de cada compilación, leídos con la clave de App Store Connect del espacio de trabajo. No hay que instalar nada.",
      generic: "Una fuente de {kind} leería estas señales para esta app.",
    },
    credentialMissing: {
      google_play: "Guarda antes la credencial de Google Play.",
      asc: "Guarda antes la clave de App Store Connect.",
    },
    saveCredential: "Ajustes → Integraciones",
    connect: "Conectar",
    connecting: "Conectando…",
    notNow: "Ahora no",
    recipe: "Receta {recipe}",
  },
  binding: {
    noPreviewTitle: "No hay vista previa para esta señal",
    noPreview: "{signal} no es una serie que se pueda representar: los logs, las trazas y los grupos de errores se leen en el explorador de logs, en Trazas y en la Bandeja de errores. Guarda para vincularla.",
  },
  errors: {
    providers: {
      sentry: "Sentry",
    },
    fromProvider: "Leído de {provider}",
    noTask: "Este grupo no indica ninguna fuente de datos con la que crear una tarea. Ábrelo en {provider}.",
    openSource: "Fuente de datos de {provider}",
    fixSource: "Abrir la fuente de datos",
  },
  sources: {
    storeCredentials: {
      google_play: "Cuenta de servicio de Google Play",
      asc: "Clave de API de App Store Connect",
    },
    storeCredentialWorkspace: "guardada para el espacio de trabajo",
    storeCredentialPlaceholder: "Elige la {credential}",
    noStoreCredential: "Todavía no hay ninguna {credential} guardada.",
    saveStoreCredential: "Guárdala en Ajustes → Integraciones",
    namePlaceholderStore: "Si lo dejas vacío, el nombre del tipo",
    errors: {
      store_credential: {
        not_found: "Esa credencial aún no está guardada. Guárdala antes en Ajustes → Integraciones.",
        wrong_provider: "Esa credencial es de la otra tienda.",
        not_permitted: "En esta instalación no se pueden usar credenciales de tienda para las fuentes de datos.",
      },
      org: {
        invalid: "Introduce el slug de la organización, como en sentry.io/organizations/<slug>/.",
      },
      index: {
        invalid: "Introduce un índice, un data stream o un patrón, como logs-*; varios, separados por comas.",
      },
    },
  },
};

const AUTH_OPTIONS = {
  none: "Ninguna",
  bearer: "Token bearer",
  basic: "Básica (usuario y contraseña)",
};

export const telemetryKinds: TelemetryKindsDict = {
  google_play_vitals: {
    label: "Google Play vitals",
    description:
      "Android vitals de Google Play (tasas de fallos y de ANR, arranques lentos y los principales grupos de fallos), leídos con la credencial de Google Play del espacio de trabajo. No hay que instalar nada; Play los publica con uno o dos días de retraso.",
    queryPlaceholder: 'p. ej. {"metric_set": "crashRateMetricSet", "metric": "userPerceivedCrashRate", "as": "percent"}',
    queryHint:
      'JSON: {"package"?, "metric_set", "metric", "dimension"?, "filter"?, "period"?, "as"?}. metric_set es crashRateMetricSet, anrRateMetricSet, slowStartRateMetricSet o errorCountMetricSet; period es daily (por defecto) o hourly; as es value, percent o free_percent. Los datos diarios llevan 1–2 días de retraso.',
    fields: {
      store_credential: {
        label: "Credencial de Google Play",
        help: "La cuenta de servicio guardada en Ajustes → Integraciones → Google Play. Necesita la Play Developer Reporting API activada en su proyecto de Google Cloud y, en Play Console, permiso para ver la información de la app.",
      },
      package_name: {
        label: "Nombre del paquete",
        help: "La app que se lee cuando una consulta no indica ninguna. La vinculación de un componente lo rellena con la app de Android vinculada a su repositorio.",
        placeholder: "com.example.app",
      },
    },
  },
  app_store_metrics: {
    label: "Métricas de App Store Connect",
    description:
      "El tiempo de arranque, los bloqueos, la memoria, las escrituras en disco y el consumo de batería de una app de iOS tal como los muestra Xcode Organizer, por versión de la app, y los diagnósticos de bloqueos y arranques de cada compilación, leídos con la clave de App Store Connect del espacio de trabajo. Apple no informa de una tasa sin fallos.",
    queryPlaceholder: 'p. ej. {"metric": "launchTime", "percentile": "p90"}',
    queryHint:
      'JSON: {"app"?, "metric", "percentile"?, "device"?, "version"?}. metric es una métrica de Xcode Organizer (launchTime, hangRate, …) o una categoría entera (launch, hang, memory, disk, battery); percentile es p50 o p90. Los valores llegan por versión de la app y se dibujan como escalones.',
    fields: {
      store_credential: {
        label: "Credencial de App Store Connect",
        help: "La clave de API guardada en Ajustes → Integraciones → App Store Connect.",
      },
      app_id: {
        label: "ID de la app",
        help: "La app que se lee cuando una consulta no indica ninguna: su ID de App Store Connect. La vinculación de un componente lo rellena con la app de iOS vinculada a su repositorio.",
        placeholder: "1234567890",
      },
    },
  },
  sentry: {
    label: "Sentry",
    description:
      "Usuarios y sesiones sin fallos, recuentos de errores y grupos de errores de Sentry, tanto para proyectos móviles como web y de backend. Sus grupos de errores se suman a la Bandeja de errores.",
    queryPlaceholder: 'p. ej. {"project": "ios-app", "metric": "crash_free_users"}',
    queryHint:
      'JSON: {"project", "metric"?, "environment"?, "release"?, "search"?, "days"?}. metric es events (por defecto), event_rate, crash_free_users, crash_free_sessions, crash_rate, crash_rate_users, sessions o users; las métricas de fallos necesitan el seguimiento de sesiones activado en el SDK.',
    fields: {
      url: {
        label: "URL de Sentry",
        help: "https://sentry.io para la nube de Sentry (https://de.sentry.io para una organización de la región de la UE), o la dirección de tu Sentry autoalojado.",
        placeholder: "https://sentry.io",
      },
      org: {
        label: "Organización",
        help: "El slug de la organización, como en sentry.io/organizations/<slug>/.",
        placeholder: "acme",
      },
      token: {
        label: "Token de autenticación",
        help: "Un token con los scopes org:read, project:read y event:read: un token de organización o el de una integración interna. Se guarda cifrado.",
      },
    },
  },
  loki: {
    label: "Loki",
    description: "Grafana Loki o Grafana Cloud Logs: las líneas de log de un componente y las tasas de error que se cuentan a partir de ellas con LogQL.",
    queryPlaceholder: 'p. ej. {service_name="api"} |= "error"',
    queryHint:
      'LogQL. Una consulta de logs ({service_name="api"} |= "timeout") alimenta el explorador de logs; una consulta de métricas (sum(rate({service_name="api"} | detected_level="error" [5m]))) dibuja un gráfico.',
    fields: {
      url: {
        help: "La URL base de Loki, sin /loki/api/v1. En Grafana Cloud, la URL de Logs (https://logs-prod-<n>.grafana.net).",
        placeholder: "http://localhost:3100",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "En Grafana Cloud, el ID de usuario de Logs.",
      },
      password: {
        help: "En Grafana Cloud, un token de access policy con logs:read.",
      },
    },
  },
  elasticsearch: {
    label: "Elasticsearch / OpenSearch",
    description: "Líneas de log guardadas en Elasticsearch, OpenSearch o Elastic Cloud: un patrón de índice, filtrado por campo.",
    queryPlaceholder: 'p. ej. {"filters": {"service.name": "billing"}}',
    queryHint: 'JSON: {"filters": {"<field>": "<value>", …}}; tienen que cumplirse todos los filtros, 32 como máximo. En un campo keyword la coincidencia es exacta; en uno text, tienen que aparecer todas las palabras.',
    fields: {
      url: {
        help: "La URL base de Elasticsearch u OpenSearch. En Elastic Cloud, el endpoint de Elasticsearch del despliegue.",
        placeholder: "http://localhost:9200",
      },
      index: {
        label: "Índice",
        help: "El índice, data stream o patrón que contiene los logs; varios, separados por comas (logs-*,app-*).",
        placeholder: "logs-*",
      },
      time_field: {
        label: "Campo de tiempo",
        help: "El campo de fecha por el que se ordena y filtra cada línea. Si lo dejas vacío, @timestamp.",
        placeholder: "@timestamp",
      },
      message_field: {
        label: "Campo del mensaje",
        help: "El campo que contiene la propia línea; el filtro de texto busca en él. Si lo dejas vacío, message.",
        placeholder: "message",
      },
      level_field: {
        label: "Campo del nivel",
        help: "El campo que contiene el nivel (log.level en ECS). Si lo dejas vacío, log.level.",
        placeholder: "log.level",
      },
      auth: {
        options: {
          none: "Ninguna",
          basic: "Básica (usuario y contraseña)",
          api_key: "Clave de API",
        },
      },
      api_key: {
        label: "Clave de API",
        help: "La clave de API codificada (base64 de id:key), tal como la muestra Kibana al crearla. Se guarda cifrada.",
      },
    },
  },
  tempo: {
    label: "Tempo",
    description: "Grafana Tempo o Grafana Cloud Traces: las trazas de un componente, buscadas con TraceQL.",
    queryPlaceholder: 'p. ej. { resource.service.name = "api" }',
    queryHint: 'TraceQL: los spans desde los que empiezan las trazas de un componente, como { resource.service.name = "api" }. Se leen en Trazas, nunca como gráfico.',
    fields: {
      url: {
        help: "La base de la API HTTP de Tempo, sin /api. En Grafana Cloud, la URL de Traces que termina en /tempo.",
        placeholder: "http://localhost:3200",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "En Grafana Cloud, el ID de usuario de Traces.",
      },
      password: {
        help: "En Grafana Cloud, un token de access policy con traces:read.",
      },
    },
  },
  posthog: {
    label: "PostHog",
    description:
      "Páginas vistas, sesiones, visitantes, excepciones de JavaScript y web vitals de PostHog, con las principales páginas, referentes, países y dispositivos. No hay que instalar nada aparte de posthog-js.",
    queryPlaceholder: 'p. ej. {"metric": "lcp", "quantile": 0.75}',
    queryHint:
      'JSON: {"metric"?, "event"?, "quantile"?, "host"?}. metric es page_views (por defecto), sessions, visitors, js_errors, events (con el event que cuenta) o una web vital: lcp, inp, cls o fcp, en el cuantil 0.75 salvo que se indique otro. host se queda con un solo nombre de host.',
    fields: {
      url: {
        label: "URL de PostHog",
        help: "https://us.posthog.com o https://eu.posthog.com para PostHog Cloud, o la dirección de tu PostHog autoalojado.",
        placeholder: "https://us.posthog.com",
      },
      project_id: {
        label: "ID del proyecto",
        help: "El número de la URL del proyecto (/project/<id>) o el que aparece en Project settings.",
        placeholder: "12345",
      },
      personal_api_key: {
        label: "Clave de API personal",
        help: "Una clave de API personal (phx_…) con el scope Query Read en este proyecto. Se guarda cifrada.",
      },
    },
  },
  ga4: {
    label: "Google Analytics 4",
    description:
      "Páginas vistas, sesiones y usuarios activos de una propiedad de Google Analytics 4, con las principales páginas, referentes, países y dispositivos. GA4 tarda hasta dos días en procesar los datos, así que los puntos más recientes aún pueden subir.",
    queryPlaceholder: 'p. ej. {"metric": "page_views"}',
    queryHint:
      'JSON: {"metric"?, "event"?, "host"?, "property"?}. metric es page_views (por defecto), sessions, visitors o events (con el event que cuenta, el eventName de GA4). property lee una propiedad distinta de la de la fuente.',
    fields: {
      property_id: {
        label: "ID de la propiedad",
        help: "El número que aparece en Admin → Property details (no el ID de medición G-…).",
        placeholder: "123456789",
      },
      auth: {
        label: "Iniciar sesión con",
        help: "Una cuenta de Google Cloud conectada o una clave de cuenta de servicio solo para esta fuente.",
        options: {
          cloud_account: "Una cuenta de Google Cloud conectada",
          service_account: "Una clave de cuenta de servicio",
        },
      },
      cloud_account_id: {
        label: "Cuenta de Google Cloud",
        help: "Su cuenta de servicio tiene que tener el rol Viewer en la propiedad (Admin → Property access management) y la Google Analytics Data API activada en su proyecto.",
      },
      service_account_json: {
        label: "Clave de cuenta de servicio (JSON)",
        help: "Una clave de una cuenta de servicio con el rol Viewer en la propiedad y la Google Analytics Data API activada en su proyecto. Pega el archivo JSON completo. Se guarda cifrada.",
      },
    },
  },
  plausible: {
    label: "Plausible",
    description: "Páginas vistas, visitas y visitantes de Plausible Analytics, con las principales páginas, fuentes, países y dispositivos.",
    queryPlaceholder: 'p. ej. {"metric": "visitors"}',
    queryHint: 'JSON: {"metric"?, "host"?, "site"?}. metric es page_views (por defecto), sessions (visitas) o visitors. site lee un sitio distinto del de la fuente.',
    fields: {
      url: {
        label: "URL de Plausible",
        help: "https://plausible.io, o la dirección de tu Plausible autoalojado.",
        placeholder: "https://plausible.io",
      },
      site_id: {
        label: "Sitio",
        help: "El dominio del sitio tal como se llama en Plausible.",
        placeholder: "example.com",
      },
      api_key: {
        label: "Clave de la Stats API",
        help: "Una clave de la Stats API (Account settings → API keys) del equipo al que pertenece el sitio. Se guarda cifrada.",
      },
    },
  },
  datadog: {
    label: "Datadog",
    description: "Tasas de peticiones, errores y latencia de APM, y métricas de hosts o contenedores, desde la API de métricas de Datadog.",
    queryPlaceholder: "p. ej. sum:trace.http.request.hits{service:api,env:prod}.as_rate()",
    queryHint:
      "Una consulta de métricas de Datadog, tal como la escribe un dashboard: avg:system.cpu.user{service:web} by {host}. Una tabla lee el último valor de cada grupo de etiquetas.",
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
        label: "Sitio",
        help: "El sitio de tu URL de Datadog: app.datadoghq.com es datadoghq.com, app.datadoghq.eu es datadoghq.eu y us5.datadoghq.com es us5.datadoghq.com.",
      },
      api_key: {
        label: "Clave de API",
        help: "Organization settings → API keys. Se guarda cifrada.",
      },
      app_key: {
        label: "Clave de aplicación",
        help: "Organization settings → Application keys; una clave con scopes limitados necesita timeseries_query y metrics_read. Se guarda cifrada.",
      },
    },
  },
  newrelic: {
    label: "New Relic",
    description: "Rendimiento, errores y latencia de APM, páginas vistas y web vitals del navegador, y tasas de fallos móviles, desde New Relic con NRQL.",
    queryPlaceholder: "p. ej. SELECT percentile(duration * 1000, 95) FROM Transaction WHERE appName = 'api'",
    queryHint:
      "NRQL, sin SINCE, UNTIL ni TIMESERIES: el tipo los añade según el intervalo. Una tabla es una consulta FACET, con una fila por faceta.",
    fields: {
      region: {
        label: "Región",
        help: "Dónde están los datos de la cuenta: eu para one.eu.newrelic.com; si no, us.",
        options: {
          us: "US",
          eu: "EU",
        },
      },
      account_id: {
        label: "ID de la cuenta",
        help: "El número de la cuenta, que aparece bajo su nombre en el selector de cuentas.",
        placeholder: "1234567",
      },
      api_key: {
        label: "Clave de usuario",
        help: "Una clave de usuario (NRAK-…) de API keys, de un usuario que pueda consultar la cuenta. Se guarda cifrada.",
      },
    },
  },
};
