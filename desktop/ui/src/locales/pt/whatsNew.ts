// Dicionário em português da janela "Novidades"; mesma forma que en/whatsNew.ts.
export const whatsNew = {
  dialogTitle: "Novidades do TaskTrooper {version}",
  seeAll: "Ver todas as mudanças",
  gotIt: "Entendi",
  card: {
    title: "Novidades",
    help: "Veja o que mudou na versão mais recente do TaskTrooper.",
    open: "Mostrar novidades",
  },
  v0_3_20: {
    title: "TaskTrooper Telemetry em Operações, um designer que desenha no FigCraft, marcadores de deploy",
    items: {
      operationsSource: "Conecte o TaskTrooper Telemetry em Configurações e ele vira sozinho uma fonte de dados de Operações: gráficos RED, logs, traces, erros e endpoints de cada componente.",
      agentTools: "Os agentes recebem as ferramentas de telemetria que sua função precisa: análise de causa raiz por endpoint, SLOs, monitores e testes de carga. Só podem alterar ou excluir monitores e KPIs criados por um agente.",
      deployMarkers: "Cada deploy que entra no ar é marcado na telemetria conectada com seu commit, para que uma regressão apareça ao lado da versão que a causou.",
      figcraftDesigner: "O ui-designer desenha no FigCraft: cada tarefa de design ganha seu próprio arquivo editável baseado no seu design system, cujas páginas você pode visualizar, abrir no FigCraft ou mostrar no Finder pela tarefa. FigCraft, VectorCraft e PhotoCraft são instalados em Configurações → Ambiente de desenvolvimento.",
      telemetryScreens: "Novas telas de telemetria: visão geral, mapa de serviços, infraestrutura, apps, profiling, uso de LLM e testes de carga.",
    },
  },
  v0_3_19: {
    title: "QA que testa e conclui, testes automatizados por repositório, uma prévia por tarefa",
    items: {
      qaManualRound: "A QA agora testa o produto em execução segundo os critérios da tarefa em vez de compilá-lo: build e testes unitários ficam com a revisão de código.",
      qaFinishes: "Uma rodada de QA não pode terminar com um caso não executado ou um critério sem veredito, nem devolver uma tarefa sem uma falha registrada.",
      automationTests: "Ative Testes automatizados em um repositório (Componentes → Revisão): a QA adiciona os casos de cada tarefa como testes Testcontainers + WireMock em qa-automation/, e uma tarefa só sai da QA com essa suíte verde. Ativar abre uma tarefa que escreve os testes que faltam.",
      taskPreviews: "Cada tarefa ganha sua própria prévia local em sua própria porta, e mover uma tarefa para só a prévia dela.",
    },
  },
  v0_3_18: {
    title: "Fale com seus agentes, anexe e marque em todo lugar, planos de tarefas mais enxutos",
    items: {
      voiceChat: "Fale em voz alta com os agentes em turco ou inglês: a resposta é lida enquanto é escrita, os desenhos aparecem na tela e você pode interromper falando. Escolha o modelo e a voz em Configurações → Voz.",
      attachAndMention: "Cole ou anexe arquivos e imagens, e marque agentes, projetos e repositórios com @, no Início, na pergunta rápida do topo e nos comentários de tarefas.",
      notifications: "A central de notificações mostra quem enviou cada notificação com o avatar, no seu idioma, e o selo da barra lateral conta o que está realmente não lido.",
      onePerFeature: "O PM abre uma tarefa por funcionalidade, repositório e papel em vez de muitas pequenas, para que cada funcionalidade seja construída, revisada e lançada uma vez.",
      releaseBatch: "O release engineer pode propor um lançamento em lote para você aprovar e pergunta passo a passo como um componente é implantado.",
    },
  },
  v0_3_17: {
    title: "Prévias hospedadas primeiro, capturas que os agentes veem, sem pingue-pongue de QA",
    items: {
      hostedPreview: "Quando o pull request de uma tarefa tem uma prévia hospedada pronta (Vercel), o painel de prévia a abre primeiro em vez de iniciar uma execução local.",
      chatImages: "As capturas de tela anexadas num chat com agente agora chegam aos agentes que usam Claude Code, opencode, Cursor e Antigravity, que conseguem ler o que mostram.",
      qaBlocked: "Quando o QA não consegue verificar algo porque falta no ambiente (uma integração, credencial ou dispositivo), a tarefa espera por você em vez de ir e voltar entre QA e desenvolvedor; o QA não testa de novo código já rejeitado e inalterado.",
    },
  },
  v0_3_16: {
    title: "Testes com audiência antes do trabalho, novas execuções que comparam, tarefas completas",
    items: {
      studyRerun: "Execute de novo um estudo concluído depois de mudar o produto e veja o que mudou: a pontuação do veredito, as objeções que sumiram ou surgiram e a mudança de cada pessoa.",
      pmAudienceTest: "Quando um pedido depende de como as pessoas reagem (um preço, um recurso pago, textos, onboarding), o PM oferece um teste com audiência antes de abrir o trabalho, deixa você revisar e testar de novo e abre as tarefas quando você pedir.",
      serialReleases: "As versões saem uma de cada vez por componente, então dois deploys do mesmo serviço nunca se sobrepõem.",
      completeTasks: "As tarefas abertas pelos agentes sempre têm descrição, critérios de aceitação e um responsável; uma tarefa vazia ou sem responsável é recusada e corrigida pelo agente.",
      chatThinking: "Enquanto um agente trabalha, o raciocínio vai direto para as etapas de raciocínio em vez de aparecer primeiro como resposta; o arquiteto de sistemas responde perguntas técnicas a partir do código.",
      mascot: "Trooper, o novo mascote, aparece em telas vazias, na configuração e nos avatares dos agentes.",
    },
  },
  v0_3_15: {
    title: "Agentes com o guia certo, tarefas com um agente e comandos Docker",
    items: {
      agentsMd: "A tarefa de guia para agentes da configuração do projeto agora escreve um único AGENTS.md para qualquer CLI de agente e não pede mais um README que o projeto já tem.",
      assignTasks: "Tarefas abertas por uma varredura ou por um agente recebem um agente adequado ao serem criadas, e o PM pode definir ou corrigir o responsável de uma tarefa no próprio lugar, sem recriá-la.",
      dockerCommands: "Componentes em contêineres ganham comandos Docker (buildkit, build de imagem, compose up, down e stop), configurações de registro, plataformas e tag, uma regra para os agentes trabalharem só dentro dos contêineres, e qualquer comando pode ser removido.",
    },
  },
  v0_3_14: {
    title: "Pesquisa de audiência no seu telefone",
    items: {
      phoneAudience: "Gerencie audiências no app móvel do TaskTrooper: crie com o PM ou por um formulário, construa e converse individualmente com qualquer pessoa.",
      phoneStudies: "Execute estudos pelo telefone: escolha o tipo, revise e aprove o roteiro das entrevistas, acompanhe as entrevistas e gere o relatório.",
      phoneReports: "Leia os relatórios dos estudos no telefone e compartilhe como um único arquivo HTML.",
    },
  },
  v0_3_13: {
    title: "Seu telefone, seu público e sua telemetria",
    items: {
      tunnel: "Entre no Cloudflare ou no ngrok em Configurações → Acesso remoto e acesse este computador pelo app móvel do TaskTrooper através de um túnel que você controla. Pareie um telefone em uma etapa: Parear telefone abre uma janela com o código QR e o endereço.",
      audience: "Pesquisa de público: peça ao agente PM personas sintéticas ou execute você mesmo um estudo - entrevistas, teste de landing page, teste de mensagens ou teste A/B - leia o relatório e exporte-o como um único arquivo HTML.",
      telemetry: "As novas telas de Telemetria em Operações mostram serviços, endpoints, traces, logs, erros, incidentes, monitores, deploys e SLOs, e o engenheiro de release cuida dos incidentes gerados.",
      repoSetup: "Importar um repositório agora abre a lista «Deixe este projeto pronto» (escaneamento, ferramentas, índice, README e guia para agentes, testes, CI, entrega) com um botão por passo e outro para executar todos em ordem. Sair da sua conta agora a apaga por completo, então o aviso \"trabalhando sem conta\" não fica mais na tela.",
      runTrace: "Cada execução de um agente agora deixa um rastro real no painel da tarefa: o que leu, alterou e executou, verificado com o git. Após uma prévia, um clique desfaz o trabalho da execução atrás de um branch de backup.",
      reviewHistory: "Cada tarefa agora traz o seu histórico de revisão: cada rodada de revisão e de QA com quem revisou, o que encontrou e o que aconteceu com isso, os retornos entre colunas e o que cada revisor cobriu ou não. Um cartão \"Precisa da sua revisão\" lista o que os revisores e o servidor querem que uma pessoa veja.",
    },
  },
};
