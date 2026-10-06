import type { Dict } from "@/locales/en";
import { activityArea } from "@/locales/pt/activityArea";
import { addRepository } from "@/locales/pt/addRepository";
import { agentArea } from "@/locales/pt/agentArea";
import { analysisReview } from "@/locales/pt/analysisReview";
import { boardArea } from "@/locales/pt/boardArea";
import { chatArea } from "@/locales/pt/chatArea";
import { cloud } from "@/locales/pt/cloud";
import { content } from "@/locales/pt/content";
import { frame } from "@/locales/pt/frame";
import { lib } from "@/locales/pt/lib";
import { operations } from "@/locales/pt/operations";
import { projectAdmin } from "@/locales/pt/projectAdmin";
import { projectModel } from "@/locales/pt/projectModel";
import { projectsHub } from "@/locales/pt/projectsHub";
import { release } from "@/locales/pt/release";
import { repositoryPage } from "@/locales/pt/repositoryPage";
import { settingsPages } from "@/locales/pt/settingsPages";
import { setup } from "@/locales/pt/setup";

// Brazilian Portuguese dictionary. Typed as Dict — must mirror en.ts keys exactly.
export const pt: Dict = {
  common: {
    save: "Salvar",
    saving: "Salvando...",
    cancel: "Cancelar",
    refresh: "Atualizar",
    resetDefault: "Restaurar padrão",
    actionFailed: "A ação falhou",
    saved: "Salvo",
    saveFailed: "Falha ao salvar",
    comingSoon: "Em breve",
    errorBoundary: {
      title: "Algo deu errado",
      body: "Ocorreu um erro inesperado e não foi possível renderizar a tela. Tente recarregar a página.",
      retry: "Tentar novamente",
      reload: "Recarregar página",
    },
    configError: {
      title: "Erro de configuração",
      body: "Esta build não tem uma chave de API para o servidor local, então todas as requisições seriam recusadas. Defina VITE_API_KEY (deve ser igual à SERVER_API_KEY do servidor) e gere a build novamente.",
      missing: "Ausente:",
    },
  },
  settings: {
    language: {
      label: "Idioma",
      help: "Usado nas respostas do assistente e nas instruções do sistema.",
    },
    loadFailed: "Falha ao carregar as configurações",
    savedToast: "Configurações salvas",
    github: {
      statusUnavailable: "Status indisponível.",
      connected: "✓ Conectado: {login} — os agentes podem criar repositórios privados, fazer push e abrir PRs de rascunho.",
      disconnect: "Desconectar",
      connect: "Salvar token",
      tokenPlaceholder: "ghp_… ou github_pat_…",
      tokenHelp: "Um token de acesso pessoal criado em GitHub → Settings → Developer settings. Um token clássico precisa de repo, workflow, admin:repo_hook e read:org; um fine-grained precisa de Contents, Pull requests, Workflows (read and write) e Webhooks. Sem workflow, nenhum agente consegue adicionar ou alterar um arquivo de CI. Ele é verificado no GitHub antes de ser armazenado, criptografado, nesta máquina.",
      connectedToast: "GitHub conectado",
      connectFailedToast: "Falha ao conectar o GitHub",
      disconnectedToast: "Conexão com o GitHub removida",
      missingScopesTitle: "Faltam escopos neste token: {scopes}",
      missingWorkflowScope: "Sem o escopo workflow, o GitHub recusa todo push que adiciona ou altera um arquivo em .github/workflows, então nenhum agente consegue configurar ou corrigir o CI. Crie um token com esse escopo, desconecte e salve o novo.",
      fineGrainedHint: "Token fine-grained: verifique se ele tem Workflows: Read and write, senão o GitHub recusa pushes que mexem em arquivos de CI.",
      connectWithGitHub: "Conectar com o GitHub",
      connectWithGitHubHint: "Entre no github.com com um código de uso único. O TaskTrooper renova a conexão sozinho — sem token para criar ou colar.",
      deviceCodeTitle: "Digite este código no GitHub",
      deviceCodeHint: "Abra {url}, entre, digite o código e aprove o TaskTrooper. Esta janela continua sozinha.",
      copyCode: "Copiar código",
      codeCopied: "Código copiado",
      openGitHub: "Abrir o GitHub",
      waitingForApproval: "Aguardando sua aprovação…",
      flowExpired: "O código expirou antes de ser aprovado.",
      flowDenied: "O acesso foi recusado no GitHub.",
      tryAgain: "Tentar novamente",
      cancel: "Cancelar",
      useTokenInstead: "Usar um token de acesso pessoal",
      useAppInstead: "Conectar com o GitHub",
      modeApp: "via login no GitHub",
      modeToken: "via token de acesso",
      needsInstallTitle: "Instale o app nos seus repositórios",
      needsInstallBody: "A conexão funciona, mas o app TaskTrooper ainda não está instalado em nenhuma conta, então não alcança nenhum repositório. Instale-o na sua conta ou organização e escolha All repositories, para que os repositórios que o TaskTrooper criar depois também sejam incluídos.",
      installApp: "Instalar no GitHub",
      expiredTitle: "A conexão com o GitHub expirou",
      expiredBody: "Ela não foi usada por seis meses ou foi revogada no GitHub. Conecte novamente.",
    },
    boilerplate: {
      title: "Catálogo de boilerplates",
      descPrefix: "Antes de escrever código do zero, os agentes consultam",
      descMid: "neste repositório; se existir um boilerplate correspondente, eles começam copiando-o.",
      descSuffix: "ou uma URL completa também é aceita.",
      loadFailed: "Falha ao carregar as configurações",
    },
    notifications: {
      title: "Notificações na área de trabalho",
      help: "Notificações nativas para eventos do quadro que precisam da sua atenção. Elas continuam funcionando depois que você fecha a janela.",
      unavailable: "As notificações na área de trabalho só estão disponíveis no app TaskTrooper, não no navegador.",
      enabled: "Ativadas",
      analizReview: "Análise pronta para sua revisão",
      humanUat: "Aguardando seu UAT",
      humanNeeded: "Um agente precisa de uma decisão humana",
      agentComments: "Novos comentários de agentes",
      agentChatReplies: "Respostas de agentes no chat",
      loadFailed: "Falha ao carregar as configurações",
      saveFailed: "Falha ao salvar",
    },
    concurrency: {
      title: "Limites de concorrência",
      agentsLabel: "Máx. de agentes simultâneos",
      tasksLabel: "Máx. de tarefas simultâneas",
      agentsHelp: "Quantas execuções de agente podem rodar ao mesmo tempo a partir do quadro.",
      tasksHelp: "Quantas tarefas diferentes podem ter um agente em execução ao mesmo tempo.",
      zeroHint: "0 significa ilimitado",
      reset: "Ilimitado",
      resetting: "Redefinindo…",
      loadFailed: "Falha ao carregar as configurações",
      saveFailed: "Falha ao salvar",
    },
  },
  addRepository,
  agentArea,
  analysisReview,
  boardArea,
  activityArea,
  chatArea,
  cloud,
  content,
  frame,
  lib,
  operations,
  projectAdmin,
  projectModel,
  projectsHub,
  release,
  repositoryPage,
  settingsPages,
  setup,
};
