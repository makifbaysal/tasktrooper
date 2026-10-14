// Spanish dictionary for the developer-tool installer. Must mirror
// en/toolchain.ts keys exactly.
export const toolchain = {
  stepState: {
    queued: "En cola",
    running: "Instalando",
    waiting: "Esperándote",
    done: "Instalado",
    failed: "Falló",
    skipped: "Omitido",
    cancelled: "Cancelado",
  },
  itemStatus: {
    installed: "Instalado",
    installedVersion: "Instalado · {version}",
    missing: "Falta",
    unusable: "No utilizable",
  },
  badge: {
    running: "Configurando · {percent} %",
    done: "Configuración completa",
    failed: "Configuración: {count} fallidos",
    stopped: "Configuración detenida",
    incomplete: "Configuración incompleta",
  },
  waitingHint:
    "Hay una ventana de instalación o de Terminal abierta en este ordenador. Termina allí y esto sigue solo.",
  method: "Con {method}",
  needsManual: "Necesita {label}. Instálalo primero desde su página.",
  queuedAfterCurrent: "En cola tras los pasos actuales",
  kind: {
    interactive: "Abre una ventana de instalación",
    manual: "Se instala desde su propia página",
  },
  reasons: {
    everyone: "Lo usan todos los agentes",
    agents: "Para {agents}",
  },
  progress: {
    percent: "{percent} %",
    summary: "{done} de {total} terminados",
    failed: "{count} fallidos",
  },
  actions: {
    install: "Instalar",
    openPage: "Abrir página",
    openPageFor: "Abrir página de {label}",
    stop: "Detener",
    dismiss: "Descartar",
    details: "Detalles",
    restartAgents: "Reiniciar agentes",
    checkAgain: "Comprobar de nuevo",
    installRecommended: "Instalar recomendados",
  },
  restart: {
    hint: "Parte de lo instalado solo llega a los agentes después de reiniciarlos.",
    title: "¿Reiniciar los agentes?",
    description:
      "TaskTrooper reinicia el servicio en el que se ejecutan tus agentes para que vean las herramientas nuevas. Lo que esté haciendo un agente en ese momento se interrumpe y esta ventana se recarga.",
    confirm: "Reiniciar",
    failed: "No se pudieron reiniciar los agentes",
  },
  errors: {
    installFailed: "No se pudo iniciar la instalación",
    declined: "No se instaló nada",
    stopFailed: "No se pudo detener la instalación",
    dismissFailed: "No se pudo descartar la configuración terminada",
    openPageFailed: "No se pudo abrir la página",
    reportFailed: "No se pudo comprobar este ordenador",
  },
  job: {
    title: {
      running: "Preparando este ordenador",
      complete: "Configuración terminada",
      failed: "Configuración terminada con errores",
      stopped: "Configuración detenida",
      incomplete: "Configuración terminada, pero no se instaló todo",
    },
  },
  running: {
    title: "Todavía se están instalando herramientas de desarrollo",
    body: "Puede que git o Claude Code aún estén en camino. Cuando lleguen, pulsa Comprobar de nuevo.",
  },
  settings: {
    desktopOnlyTitle: "Esta página necesita la app de escritorio de TaskTrooper",
    desktopOnlyBody:
      "Comprueba e instala herramientas de desarrollo en tu propio ordenador, y una pestaña del navegador no puede llegar a eso.",
    recommendedTitle: "Recomendado para tu equipo",
    recommendedDescription: "Con lo que mejor trabajan tus agentes activos y que este ordenador aún no tiene.",
    recommendedNone: "Todo lo que tu equipo necesita ya está en este ordenador.",
    recommendedManual:
      "Las herramientas que se instalan desde su propia página no entran en la instalación de un clic; usa Abrir página más abajo.",
    allTitle: "Todas las herramientas de desarrollo",
    allDescription: "Lo que TaskTrooper puede comprobar e instalar en este ordenador.",
    lastChecked: "Comprobado {when}.",
    checking: "Comprobando este ordenador…",
    groups: {
      basics: "Básicos",
      web: "Web",
      mobile: "Móvil",
      data: "Datos",
      games: "Juegos",
      infra: "Infraestructura y seguridad",
    },
  },
};
