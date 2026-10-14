// Spanish dictionary for code-health findings. Must mirror
// en/findings.ts keys exactly.
export const findings = {
  intro: "Se lee del código en cada escaneo: antipatrones, logging ausente o mal configurado, ajustes de depuración que llegan a producción, secretos y más. Los hallazgos que aceptes o marques como falso positivo siguen cerrados en escaneos posteriores.",
  allComponents: "Todos los componentes",
  repositoryWide: "Todo el repositorio",
  openCount: "{count} abiertos",
  allCategories: "Todas las categorías",
  occurrences: "{count} apariciones",
  lines: "{count} líneas",
  moreLocations: "y {count} más",
  showLocations: "Mostrar ubicaciones",
  hideLocations: "Ocultar ubicaciones",
  createTask: "Crear tarea",
  createTaskSelected: "Crear tarea para {count}",
  selectAll: "Seleccionar todo",
  clearSelection: "Limpiar",
  accept: "Aceptar",
  falsePositive: "Falso positivo",
  reopen: "Reabrir",
  openTask: "Abrir {key}",
  reason: "Motivo: {reason}",
  resolvedHint: "El último escaneo ya no lo encuentra.",
  updateFailed: "No se pudo actualizar el hallazgo",
  componentSummary: {
    open: "{count} hallazgos de salud del código abiertos",
    none: "Sin hallazgos de salud del código abiertos",
    view: "Ver hallazgos",
  },
  status: {
    open: "Abierto",
    accepted: "Aceptado",
    false_positive: "Falso positivo",
    resolved: "Resuelto",
  },
  severity: {
    high: "Alta",
    medium: "Media",
    low: "Baja",
  },
  category: {
    security: "Seguridad",
    configuration: "Configuración",
    logging: "Logging",
    error_handling: "Manejo de errores",
    observability: "Observabilidad",
    reliability: "Fiabilidad",
    quality: "Calidad del código",
  },
  empty: {
    noScanTitle: "Aún sin escanear",
    noScanDescription: "Los hallazgos aparecen tras el primer escaneo del repositorio.",
    cleanTitle: "No se encontraron problemas",
    cleanDescription: "El último escaneo no encontró nada que corregir aquí.",
    filtered: "Ningún hallazgo coincide con este filtro.",
  },
  verdict: {
    acceptTitle: "Aceptar el riesgo",
    acceptDescription: "El hallazgo se cierra y sigue cerrado en escaneos posteriores, aunque el código aún lo contenga.",
    falsePositiveTitle: "Marcar como falso positivo",
    falsePositiveDescription: "Indica que el escaneo se equivoca aquí. El hallazgo sigue cerrado en escaneos posteriores.",
    reasonLabel: "Motivo (opcional)",
    reasonPlaceholder: "Por qué aquí está bien",
    save: "Guardar",
  },
  task: {
    title: "Crear una tarea",
    description: "Una tarea del tablero que cubre {count} hallazgos. El agente recibe cada ubicación y cómo corregir cada uno.",
    titleLabel: "Título",
    notesLabel: "Notas para el agente (opcional)",
    notesPlaceholder: "Lo que el agente debe saber primero",
    submit: "Crear tarea",
    created: "Tarea {key} creada",
    failed: "No se pudo crear la tarea",
    defaultTitleMany: "Corregir {count} hallazgos de salud del código en {component}",
    severalComponents: "varios componentes",
  },
  rules: {
    hardcoded_secret: {
      title: "Secreto en el código",
      description: "Hay una clave o token con formato conocido en el repositorio. Rótala y léela desde la configuración.",
    },
    hardcoded_credential: {
      title: "Credencial escrita en el código",
      description: "Una contraseña, clave o token está como literal en lugar de leerse de la configuración.",
    },
    committed_env_file: {
      title: "Archivo env con secretos sin ignorar",
      description: "Un archivo .env con secretos reales está versionado o git no lo ignora.",
    },
    tls_verification_disabled: {
      title: "Verificación TLS desactivada",
      description: "La comprobación de certificado o de host está apagada y el tráfico puede interceptarse.",
    },
    cleartext_traffic_allowed: {
      title: "HTTP sin cifrar permitido",
      description: "La app permite HTTP sin cifrar para cualquier host.",
    },
    sql_injection_risk: {
      title: "Posible inyección SQL",
      description: "Un valor se inserta en una cadena SQL en lugar de pasarse como parámetro.",
    },
    dynamic_code_execution: {
      title: "Ejecución dinámica de código",
      description: "eval, exec o una deserialización insegura ejecutan lo que diga la entrada.",
    },
    unsafe_html_injection: {
      title: "HTML sin escapar",
      description: "Se renderiza HTML sin sanear, lo que permite cross-site scripting.",
    },
    permissive_cors: {
      title: "CORS permite cualquier origen",
      description: "Cualquier sitio web puede llamar a esta API desde el navegador de un visitante.",
    },
    insecure_token_storage: {
      title: "Tokens en almacenamiento plano",
      description: "Tokens o contraseñas se guardan en localStorage, UserDefaults o similar.",
    },
    http_body_logging: {
      title: "Se registran cuerpos o cabeceras",
      description: "Cuerpos y cabeceras llevan tokens y datos personales a los logs.",
    },
    error_details_exposed: {
      title: "Detalles de error enviados al cliente",
      description: "Trazas de pila o páginas de error de desarrollo llegan a los usuarios.",
    },
    public_source_maps: {
      title: "Source maps públicos",
      description: "El build de producción publica el código fuente original.",
    },
    debug_mode_in_production: {
      title: "Modo debug en producción",
      description: "La configuración de producción activa el modo debug o de desarrollo.",
    },
    debug_log_level_in_production: {
      title: "Nivel de log debug en producción",
      description: "La configuración de producción registra en nivel debug o trace.",
    },
    debuggable_release_build: {
      title: "Build release depurable",
      description: "Cualquiera puede conectar un depurador al build release de Android.",
    },
    release_minification_disabled: {
      title: "Build release sin minificar",
      description: "R8/ProGuard está desactivado en el build release.",
    },
    unconditional_debug_logging: {
      title: "Logger fijado en debug",
      description: "El nivel de log está en debug en el código e ignora el entorno.",
    },
    console_logs_in_production: {
      title: "Salida de consola de depuración en producción",
      description: "Las llamadas console.log/debug/info llegan al bundle y el build no las elimina.",
    },
    mobile_debug_prints: {
      title: "Prints de depuración en builds release",
      description: "print, NSLog o Log.d se ejecutan fuera de guardas de depuración.",
    },
    print_instead_of_logger: {
      title: "Prints en lugar de logs",
      description: "El código del servicio escribe en stdout en lugar de usar el logger.",
    },
    print_stack_trace: {
      title: "Trazas impresas, no registradas",
      description: "printStackTrace o print_exc se saltan el sistema de logs.",
    },
    no_structured_logging: {
      title: "Sin biblioteca de logging",
      description: "El servicio no tiene logger, así que no hay logs estructurados por nivel.",
    },
    no_request_logging: {
      title: "Sin logs de peticiones",
      description: "El servicio HTTP no registra las peticiones (método, estado, duración).",
    },
    catch_without_logging: {
      title: "Errores tragados sin registrar",
      description: "Los errores capturados no se registran, no se relanzan ni se reportan.",
    },
    empty_catch_block: {
      title: "Manejador de errores vacío",
      description: "Un bloque catch, except o err vacío oculta cualquier fallo en ese camino.",
    },
    bare_except: {
      title: "except desnudo",
      description: "except: también captura KeyboardInterrupt y SystemExit.",
    },
    no_error_boundary: {
      title: "Sin error boundary de React",
      description: "Un solo error de renderizado deja la interfaz en blanco y no se reporta nada.",
    },
    no_crash_reporting: {
      title: "Sin reporte de fallos o errores",
      description: "Los errores que sufren los usuarios en producción pasan desapercibidos.",
    },
    no_health_endpoint: {
      title: "Sin endpoint de health",
      description: "Las sondas y comprobaciones de despliegue no saben si el servicio está activo.",
    },
    http_client_without_timeout: {
      title: "Llamadas HTTP sin timeout",
      description: "Una dependencia lenta puede bloquear las peticiones indefinidamente.",
    },
    force_unwrap: {
      title: "Muchos force unwraps",
      description: "try!, as! y !! fallan ante un nil inesperado.",
    },
    mutable_default_argument: {
      title: "Argumento por defecto mutable",
      description: "Un valor por defecto list o dict se comparte entre llamadas.",
    },
    debug_leftovers: {
      title: "Sentencias de depuración olvidadas",
      description: "Quedan debugger, breakpoints o var_dump/dd en el código.",
    },
    oversized_source_file: {
      title: "Archivos fuente muy grandes",
      description: "Los archivos de más de 1.500 líneas son difíciles de revisar y cambiar.",
    },
    typescript_any_overuse: {
      title: "Muchos any / @ts-ignore",
      description: "La comprobación de tipos está desactivada en muchos lugares.",
    },
    tech_debt_markers: {
      title: "Muchos TODO/FIXME",
      description: "Problemas conocidos que no se siguen en ninguna parte.",
    },
    no_automated_tests: {
      title: "Sin tests automatizados",
      description: "El componente tiene código pero ningún archivo de test.",
    },
    no_linter_configured: {
      title: "Sin linter configurado",
      description: "Las desviaciones de estilo y los errores típicos no se revisan.",
    },
    wildcard_import: {
      title: "Imports con comodín",
      description: "from x import * oculta de dónde vienen los nombres.",
    },
  },
};
