// Spanish dictionary for the repository setup checklist. Must mirror
// en/readiness.ts keys exactly.
export const readiness = {
  title: "Deja este proyecto listo",
  intro: "Lo que le falta al proyecto para que los agentes trabajen bien en él, en un orden con sentido. Omite lo que no necesites.",
  progress: "{done} de {total} hechos",
  skippedCount: "{count} omitidos",
  complete: "Este proyecto está listo.",
  show: "Mostrar la lista",
  hide: "Ocultar",
  expand: "Desplegar la lista",
  collapse: "Plegar la lista",
  skip: "Omitir",
  unskip: "Deshacer omitir",
  doIt: "Hacerlo",
  open: "Abrir",
  install: "Instalar",
  viewTask: "Ver {key}",
  taskOpen: "La tarea {key} está en curso.",
  skippedNote: "Omitido.",
  waitingFor: "Espera a: {step}.",
  started: "Iniciado.",
  taskOpened: "Tarea {key} abierta en el tablero.",
  allStarted: "Se iniciaron {runs} y se abrieron {tasks} tareas.",
  actionFailed: "No se pudo hacer",
  runAll: "Ejecutar todo en orden",
  runAllTitle: "¿Ejecutar todo en orden?",
  runAllTasks: "Se abrirán {count} tareas en el tablero, cada una espera a la anterior.",
  runAllRuns: "{count} empezarán de inmediato, sin tarea.",
  runAllCost: "Cada tarea usa el tiempo de tus agentes.",
  runAllConfirm: "Ejecutar todo",
  status: {
    done: "Hecho",
    todo: "Pendiente",
    running: "En curso",
    blocked: "Espera un paso anterior",
    skipped: "Omitido",
    unknown: "Sin comprobar",
  },
  missing: {
    readme: "README",
    agent_guide: "guía para agentes",
  },
  steps: {
    scan: {
      title: "Escanear el proyecto",
      why: "El escaneo encuentra los componentes, comandos y CI de los que leen todos los demás pasos.",
    },
    toolchain: {
      title: "Instalar las herramientas de este stack",
      why: "Los agentes compilan y prueban en este equipo, así que necesita los compiladores que llaman.",
    },
    index: {
      title: "Indexar el código",
      why: "El índice deja a los agentes encontrar los archivos correctos en lugar de leerlo todo.",
    },
    guide: {
      title: "Añadir un README y una guía para agentes",
      why: "Los agentes empiezan cada tarea con estos archivos, así que son los que más tiempo y errores ahorran.",
    },
    tests: {
      title: "Hacer que las pruebas se puedan ejecutar",
      why: "Una ejecución de pruebas que pasa es como tú y los agentes saben que un cambio no rompió nada.",
    },
    ci: {
      title: "Ejecutar las comprobaciones en CI",
      why: "CI repite la compilación y las pruebas en cada cambio antes de que llegue a la rama principal.",
    },
    delivery: {
      title: "Elegir cómo se publican los cambios",
      why: "Un destino de entrega le dice a TaskTrooper adónde va un cambio terminado y cómo comprobarlo.",
    },
    observability: {
      title: "Vigilarlo en producción",
      why: "Las métricas, los logs y las trazas te avisan cuando una versión rompe algo.",
    },
    findings: {
      title: "Revisar los hallazgos de salud del código",
      why: "El escaneo marca ajustes de depuración, secretos y logging ausente que son baratos de arreglar pronto.",
    },
    design: {
      title: "Configurar el sistema de diseño",
      why: "Un sistema de diseño mantiene las pantallas nuevas coherentes con las que ya tienes.",
    },
    store: {
      title: "Vincular la app de la tienda",
      why: "Vincular la app de App Store o Google Play permite a TaskTrooper enviar builds y seguir las reseñas.",
    },
  },
  detail: {
    scan: {
      no_scan: "Aún sin escanear.",
      scan_failed: "El último escaneo falló.",
      no_components: "El escaneo no encontró componentes.",
      running: "Escaneando…",
      done: "{cmp} componentes encontrados.",
    },
    toolchain: {
      unknown: "Necesita: {languages}.",
      no_bridge: "Solo la app de escritorio puede comprobar las herramientas de este equipo.",
      missing: "Faltan en este equipo: {tools}.",
      installed: "Todo lo que necesita este stack está instalado.",
    },
    index: {
      not_indexed: "Aún sin indexar.",
      failed: "La última indexación falló.",
      unavailable: "El índice de código no está disponible.",
      running: "{processed} de {total} archivos.",
      done: "{files} archivos indexados.",
    },
    guide: {
      missing: "Falta: {missing}.",
      unreadable: "No se puede leer la copia de trabajo.",
      done: "README y guía para agentes encontrados.",
    },
    tests: {
      gaps: "Sin comando de pruebas: {components}.",
      done: "Todos los componentes tienen un comando de pruebas.",
    },
    ci: {
      no_ci: "No se encontró ningún flujo de CI.",
      done: "{checks} comprobaciones de CI encontradas.",
    },
    delivery: {
      gaps: "Sin entrega confirmada: {components}.",
      done: "Todo componente desplegable tiene un destino de entrega.",
    },
    observability: {
      gaps: "Huecos en: {components}.",
      done: "No se encontraron huecos.",
    },
    findings: {
      open: "{count} abiertos ({high} de gravedad alta).",
      done: "Sin hallazgos abiertos.",
    },
    design: {
      none: "Aún no hay sistema de diseño.",
      unavailable: "No se pudo leer el sistema de diseño.",
      done: "Hay un sistema de diseño.",
    },
    store: {
      no_app: "Ninguna app de tienda vinculada.",
      unavailable: "No se pudieron leer las apps de tienda.",
      done: "Hay una app de tienda vinculada.",
    },
  },
};
