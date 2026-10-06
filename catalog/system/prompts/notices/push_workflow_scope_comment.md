---
key: notices.push_workflow_scope_comment
version: 1
---
GitHub bu branch'in push'unu reddetti: değişiklik `.github/workflows` altındaki bir dosyaya dokunuyor ve kayıtlı GitHub token'ının workflow yetkisi yok. Commit branch'te yerelde duruyor, PR'a ulaşmadı; bu yüzden review'a geçilmedi ve görev park edildi.

Yapılacak: Ayarlar → GitHub'da workflow yetkisi olan bir token kaydet (klasik token'da `workflow` kutusu; fine-grained token'da Repository permissions → Workflows: Read and write). Sonra kartı Blocked'tan In progress'e taşı — push yeniden denenir.
