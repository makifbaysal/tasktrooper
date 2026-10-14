// Portuguese dictionary for the developer-tool installer. Must mirror
// en/toolchain.ts keys exactly.
export const toolchain = {
  stepState: {
    queued: "Na fila",
    running: "Instalando",
    waiting: "Aguardando você",
    done: "Instalado",
    failed: "Falhou",
    skipped: "Ignorado",
    cancelled: "Cancelado",
  },
  itemStatus: {
    installed: "Instalado",
    installedVersion: "Instalado · {version}",
    missing: "Ausente",
    unusable: "Inutilizável",
  },
  badge: {
    running: "Configurando · {percent}%",
    done: "Configuração concluída",
    failed: "Configuração: {count} com falha",
    stopped: "Configuração interrompida",
    incomplete: "Configuração incompleta",
  },
  waitingHint:
    "Há uma janela de instalação ou do Terminal aberta neste computador. Conclua por lá e isto continua sozinho.",
  method: "Via {method}",
  needsManual: "Precisa de {label}. Instale-o primeiro pela página dele.",
  queuedAfterCurrent: "Na fila depois das etapas atuais",
  kind: {
    interactive: "Abre uma janela de instalação",
    manual: "Instalado pela própria página",
  },
  reasons: {
    everyone: "Todos os agentes usam",
    agents: "Para {agents}",
  },
  progress: {
    percent: "{percent}%",
    summary: "{done} de {total} concluídos",
    failed: "{count} com falha",
  },
  actions: {
    install: "Instalar",
    openPage: "Abrir página",
    openPageFor: "Abrir página do {label}",
    stop: "Parar",
    dismiss: "Dispensar",
    details: "Detalhes",
    restartAgents: "Reiniciar agentes",
    checkAgain: "Verificar de novo",
    installRecommended: "Instalar recomendados",
  },
  restart: {
    hint: "Parte do que foi instalado só chega aos agentes depois que eles reiniciam.",
    title: "Reiniciar os agentes?",
    description:
      "O TaskTrooper reinicia o serviço em que seus agentes rodam para que eles vejam as novas ferramentas. O que um agente estiver fazendo agora é interrompido, e esta janela é recarregada.",
    confirm: "Reiniciar",
    failed: "Não foi possível reiniciar os agentes",
  },
  errors: {
    installFailed: "Não foi possível iniciar a instalação",
    declined: "Nada foi instalado",
    stopFailed: "Não foi possível parar a instalação",
    dismissFailed: "Não foi possível dispensar a configuração concluída",
    openPageFailed: "Não foi possível abrir a página",
    reportFailed: "Não foi possível verificar este computador",
  },
  job: {
    title: {
      running: "Preparando este computador",
      complete: "Configuração encerrada",
      failed: "Configuração encerrada com erros",
      stopped: "Configuração interrompida",
      incomplete: "Configuração encerrada, mas nem tudo foi instalado",
    },
  },
  running: {
    title: "Ferramentas de desenvolvimento ainda estão sendo instaladas",
    body: "O git ou o Claude Code ainda podem estar a caminho. Quando chegarem, clique em Verificar de novo.",
  },
  settings: {
    desktopOnlyTitle: "Esta página precisa do app de desktop do TaskTrooper",
    desktopOnlyBody:
      "Ela verifica e instala ferramentas de desenvolvimento no seu próprio computador, algo que uma aba do navegador não alcança.",
    recommendedTitle: "Recomendado para sua equipe",
    recommendedDescription: "Com o que seus agentes ativos trabalham melhor e que este computador ainda não tem.",
    recommendedNone: "Tudo de que sua equipe precisa já está neste computador.",
    recommendedManual:
      "Ferramentas instaladas pela própria página não entram na instalação com um clique; use Abrir página abaixo.",
    allTitle: "Todas as ferramentas de desenvolvimento",
    allDescription: "O que o TaskTrooper consegue verificar e instalar neste computador.",
    lastChecked: "Verificado {when}.",
    checking: "Verificando este computador…",
    groups: {
      basics: "Básico",
      web: "Web",
      mobile: "Mobile",
      data: "Dados",
      games: "Jogos",
      infra: "Infra e segurança",
    },
  },
};
