// Portuguese dictionary for the repository setup checklist. Must mirror
// en/readiness.ts keys exactly.
export const readiness = {
  title: "Deixe este projeto pronto",
  intro: "O que falta neste projeto para os agentes trabalharem bem nele, numa ordem que faz sentido. Pule o que não precisar.",
  progress: "{done} de {total} concluídos",
  skippedCount: "{count} pulados",
  complete: "Este projeto está pronto.",
  show: "Mostrar a lista",
  hide: "Ocultar",
  expand: "Expandir a lista",
  collapse: "Recolher a lista",
  skip: "Pular",
  unskip: "Desfazer pulo",
  doIt: "Fazer",
  open: "Abrir",
  install: "Instalar",
  viewTask: "Ver {key}",
  taskOpen: "A tarefa {key} está em andamento.",
  skippedNote: "Pulado.",
  waitingFor: "Aguardando: {step}.",
  started: "Iniciado.",
  taskOpened: "Tarefa {key} aberta no quadro.",
  allStarted: "{runs} iniciados e {tasks} tarefas abertas.",
  actionFailed: "Não foi possível fazer isso",
  runAll: "Executar tudo em ordem",
  runAllTitle: "Executar tudo em ordem?",
  runAllTasks: "{count} tarefas serão abertas no quadro, cada uma aguarda a anterior.",
  runAllRuns: "{count} começam já, sem tarefa.",
  runAllCost: "Cada tarefa usa o tempo dos seus agentes.",
  runAllConfirm: "Executar tudo",
  status: {
    done: "Concluído",
    todo: "A fazer",
    running: "Em andamento",
    blocked: "Aguarda um passo anterior",
    skipped: "Pulado",
    unknown: "Não verificado",
  },
  missing: {
    readme: "README",
    agent_guide: "guia para agentes",
  },
  steps: {
    scan: {
      title: "Escanear o projeto",
      why: "O escaneamento encontra os componentes, comandos e CI de que todos os outros passos dependem.",
    },
    toolchain: {
      title: "Instalar as ferramentas desta stack",
      why: "Os agentes compilam e testam neste computador, então ele precisa dos compiladores que eles chamam.",
    },
    index: {
      title: "Indexar o código",
      why: "O índice deixa os agentes encontrarem os arquivos certos em vez de ler tudo.",
    },
    guide: {
      title: "Adicionar um README e um guia para agentes",
      why: "Os agentes começam cada tarefa por estes arquivos, então são os que mais poupam tempo e erros.",
    },
    tests: {
      title: "Deixar os testes executáveis",
      why: "Uma execução de testes que passa é como você e os agentes sabem que uma mudança não quebrou nada.",
    },
    ci: {
      title: "Rodar as verificações na CI",
      why: "A CI repete o build e os testes a cada mudança antes de ela chegar à branch principal.",
    },
    delivery: {
      title: "Escolher como as mudanças vão ao ar",
      why: "Um destino de entrega diz ao TaskTrooper para onde vai uma mudança pronta e como verificá-la.",
    },
    observability: {
      title: "Acompanhar em produção",
      why: "Métricas, logs e traces avisam quando uma versão quebra algo.",
    },
    findings: {
      title: "Revisar os achados de saúde do código",
      why: "O escaneamento aponta configurações de debug, segredos e logging ausente, baratos de corrigir cedo.",
    },
    design: {
      title: "Configurar o sistema de design",
      why: "Um sistema de design mantém as telas novas coerentes com as que você já tem.",
    },
    store: {
      title: "Vincular o app da loja",
      why: "Vincular o app da App Store ou do Google Play permite ao TaskTrooper enviar builds e acompanhar avaliações.",
    },
  },
  detail: {
    scan: {
      no_scan: "Ainda não escaneado.",
      scan_failed: "O último escaneamento falhou.",
      no_components: "O escaneamento não encontrou componentes.",
      running: "Escaneando…",
      done: "{cmp} componentes encontrados.",
    },
    toolchain: {
      unknown: "Precisa de: {languages}.",
      no_bridge: "Só o app de desktop consegue verificar as ferramentas deste computador.",
      missing: "Faltam neste computador: {tools}.",
      installed: "Tudo de que esta stack precisa está instalado.",
    },
    index: {
      not_indexed: "Ainda não indexado.",
      failed: "A última indexação falhou.",
      unavailable: "O índice de código não está disponível.",
      running: "{processed} de {total} arquivos.",
      done: "{files} arquivos indexados.",
    },
    guide: {
      missing: "Falta: {missing}.",
      unreadable: "A cópia de trabalho não pode ser lida.",
      done: "README e guia para agentes encontrados.",
    },
    tests: {
      gaps: "Sem comando de teste: {components}.",
      done: "Todo componente tem um comando de teste.",
    },
    ci: {
      no_ci: "Nenhum workflow de CI encontrado.",
      done: "{checks} verificações de CI encontradas.",
    },
    delivery: {
      gaps: "Sem entrega confirmada: {components}.",
      done: "Todo componente implantável tem um destino de entrega.",
    },
    observability: {
      gaps: "Lacunas em: {components}.",
      done: "Nenhuma lacuna encontrada.",
    },
    findings: {
      open: "{count} abertos ({high} de severidade alta).",
      done: "Nenhum achado aberto.",
    },
    design: {
      none: "Ainda não há sistema de design.",
      unavailable: "Não foi possível ler o sistema de design.",
      done: "Há um sistema de design.",
    },
    store: {
      no_app: "Nenhum app da loja vinculado.",
      unavailable: "Não foi possível ler os apps da loja.",
      done: "Há um app da loja vinculado.",
    },
  },
};
