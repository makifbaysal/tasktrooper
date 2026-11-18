// Diccionario en español de la ventana "Novedades"; misma forma que en/whatsNew.ts.
export const whatsNew = {
  dialogTitle: "Novedades de TaskTrooper {version}",
  seeAll: "Ver todos los cambios",
  gotIt: "Entendido",
  card: {
    title: "Novedades",
    help: "Mira qué ha cambiado en la última versión de TaskTrooper.",
    open: "Mostrar novedades",
  },
  v0_3_20: {
    title: "TaskTrooper Telemetry en Operaciones, un diseñador que dibuja en FigCraft, marcas de despliegue",
    items: {
      operationsSource: "Conecta TaskTrooper Telemetry en Ajustes y se convierte sola en una fuente de datos de Operaciones: gráficos RED, logs, trazas, errores y endpoints de cada componente.",
      agentTools: "Los agentes reciben las herramientas de telemetría que su rol necesita: análisis de causa raíz por endpoint, SLOs, monitores y pruebas de carga. Solo pueden cambiar o borrar monitores y KPIs creados por un agente.",
      deployMarkers: "Cada despliegue que entra en producción se marca en la telemetría conectada con su commit, así una regresión aparece junto a la versión que la causó.",
      figcraftDesigner: "El ui-designer dibuja en FigCraft: cada tarea de diseño tiene su propio archivo editable basado en tu sistema de diseño, cuyas páginas puedes previsualizar, abrir en FigCraft o mostrar en Finder desde la tarea. FigCraft, VectorCraft y PhotoCraft se instalan desde Ajustes → Entorno de desarrollo.",
      telemetryScreens: "Nuevas pantallas de telemetría: resumen, mapa de servicios, infraestructura, apps, profiling, uso de LLM y pruebas de carga.",
    },
  },
  v0_3_19: {
    title: "QA que prueba y termina, pruebas automatizadas por repositorio, una vista previa por tarea",
    items: {
      qaManualRound: "QA ahora prueba el producto en ejecución según los criterios de la tarea en lugar de compilarlo: el build y las pruebas unitarias quedan en la revisión de código.",
      qaFinishes: "Una ronda de QA no puede terminar con un caso sin ejecutar o un criterio sin veredicto, ni devolver una tarea sin un fallo registrado.",
      automationTests: "Activa Pruebas automatizadas en un repositorio (Componentes → Revisión): QA añade los casos de cada tarea como pruebas Testcontainers + WireMock en qa-automation/, y una tarea solo sale de QA con esa suite en verde. Al activarlo se abre una tarea que escribe las pruebas que faltan.",
      taskPreviews: "Cada tarea tiene su propia vista previa local en su propio puerto, y mover una tarea solo detiene la suya.",
    },
  },
  v0_3_18: {
    title: "Habla con tus agentes, adjunta y etiqueta en todas partes, planes de tareas más ligeros",
    items: {
      voiceChat: "Habla en voz alta con los agentes en turco o inglés: la respuesta se lee mientras se escribe, los dibujos aparecen en pantalla y puedes interrumpir hablando. Elige el modelo y la voz en Ajustes → Voz.",
      attachAndMention: "Pega o adjunta archivos e imágenes, y etiqueta con @ agentes, proyectos y repositorios, en Inicio, en la pregunta rápida de arriba y en los comentarios de tareas.",
      notifications: "El centro de notificaciones muestra quién envió cada notificación con su avatar, en tu idioma, y la insignia de la barra lateral cuenta lo que de verdad está sin leer.",
      onePerFeature: "El PM abre una tarea por funcionalidad, repositorio y rol en lugar de muchas pequeñas, así cada funcionalidad se desarrolla, revisa y publica una vez.",
      releaseBatch: "El release engineer puede proponer un lanzamiento por lotes para que lo apruebes, y pregunta paso a paso cómo se despliega un componente.",
    },
  },
  v0_3_17: {
    title: "Primero las vistas previas alojadas, capturas que los agentes ven, sin ping-pong de QA",
    items: {
      hostedPreview: "Cuando el pull request de una tarea tiene una vista previa alojada lista (Vercel), el panel de vista previa la abre primero en lugar de iniciar una ejecución local.",
      chatImages: "Las capturas que adjuntas en un chat con un agente ahora llegan a los agentes que usan Claude Code, opencode, Cursor y Antigravity, que pueden leer lo que muestran.",
      qaBlocked: "Cuando QA no puede verificar algo porque falta en el entorno (una integración, credencial o dispositivo), la tarea te espera en lugar de ir y venir entre QA y el desarrollador; QA ya no vuelve a probar código que ya rechazó sin cambios.",
    },
  },
  v0_3_16: {
    title: "Pruebas con audiencia antes del trabajo, repeticiones que comparan, tareas completas",
    items: {
      studyRerun: "Vuelve a ejecutar un estudio terminado después de cambiar el producto y mira qué cambió: la puntuación del veredicto, las objeciones que desaparecieron o surgieron y el cambio de cada persona.",
      pmAudienceTest: "Cuando una petición depende de cómo reaccione la gente (un precio, una función de pago, textos, onboarding), el PM ofrece una prueba con audiencia antes de abrir el trabajo, te deja revisar y volver a probar, y abre las tareas cuando lo indicas.",
      serialReleases: "Las versiones se publican de una en una por componente, así dos despliegues del mismo servicio nunca se solapan.",
      completeTasks: "Las tareas que abren los agentes siempre llevan descripción, criterios de aceptación y un responsable; una tarea vacía o sin asignar se rechaza y el agente la corrige.",
      chatThinking: "Mientras un agente trabaja, su razonamiento fluye a los pasos de razonamiento en lugar de aparecer primero como respuesta; el arquitecto de sistemas responde preguntas técnicas a partir del código.",
      mascot: "Trooper, la nueva mascota, aparece en estados vacíos, en la configuración y en los avatares de los agentes.",
    },
  },
  v0_3_15: {
    title: "Los agentes tienen la guía correcta, las tareas un agente y Docker comandos",
    items: {
      agentsMd: "La tarea de guía para agentes de la configuración del proyecto ahora escribe un único AGENTS.md para cualquier CLI de agente y ya no pide un README que el proyecto ya tiene.",
      assignTasks: "Las tareas que abre un análisis o un agente reciben un agente adecuado al crearse, y el PM puede fijar o corregir el responsable de una tarea sin recrearla.",
      dockerCommands: "Los componentes en contenedores tienen comandos de Docker (buildkit, construir imagen, compose up, down y stop), ajustes de registro, plataformas y etiqueta, una regla para que los agentes trabajen solo dentro de contenedores, y cualquier comando se puede quitar.",
    },
  },
  v0_3_14: {
    title: "La investigación de audiencias, en tu teléfono",
    items: {
      phoneAudience: "Gestiona audiencias desde la app móvil de TaskTrooper: créalas con el PM o con un formulario, constrúyelas y habla uno a uno con cualquier persona.",
      phoneStudies: "Ejecuta estudios desde el teléfono: elige el tipo, revisa y aprueba el guion de las entrevistas, sigue las entrevistas y genera el informe.",
      phoneReports: "Lee los informes de los estudios en el teléfono y compártelos como un único archivo HTML.",
    },
  },
  v0_3_13: {
    title: "Tu teléfono, tu audiencia y tu telemetría",
    items: {
      tunnel: "Inicia sesión en Cloudflare o ngrok desde Ajustes → Acceso remoto y llega a este ordenador desde la app móvil de TaskTrooper mediante un túnel que controlas tú. Vincula un teléfono en un paso: Vincular teléfono abre una ventana con el código QR y la dirección.",
      audience: "Investigación de audiencia: pide al agente PM personas sintéticas o ejecuta tú mismo un estudio - entrevistas, prueba de landing page, prueba de mensajes o prueba A/B - lee el informe y expórtalo como un solo archivo HTML.",
      telemetry: "Las nuevas pantallas de Telemetría en Operaciones muestran servicios, endpoints, trazas, registros, errores, incidentes, monitores, despliegues y SLO, y el ingeniero de versiones atiende los incidentes que se generan.",
      repoSetup: "Importar un repositorio ahora abre la lista «Deja este proyecto listo» (escaneo, herramientas, índice, README y guía para agentes, pruebas, CI, entrega) con un botón por paso y otro para ejecutarlos todos en orden. Cerrar sesión de tu cuenta ahora la borra por completo, así que el aviso de \"trabajando sin cuenta\" ya no se queda en pantalla.",
      runTrace: "Cada ejecución de un agente deja ahora una traza real en el panel de la tarea: qué leyó, cambió y ejecutó, verificado con git. Tras una vista previa, un clic deshace el trabajo de la ejecución con una rama de respaldo.",
      reviewHistory: "Cada tarea ahora lleva su historial de revisión: cada ronda de revisión y de QA con quién revisó, qué encontró y qué pasó con ello, las devoluciones entre columnas y qué cubrió y qué no cubrió cada revisor. Una tarjeta \"Necesita tu revisión\" lista lo que los revisores y el servidor quieren que mire una persona.",
    },
  },
};
