---
name: testflight-shipper
description: Archive + upload в TestFlight. Диспатч ТОЛЬКО по явной фразе «ship to testflight». НЕ автоматически.
tools: Bash, Read
model: haiku
return_format: |
  verdict: done|failed
  artifact: <path к upload-log.txt или build number>
  next: null
  one_line: <≤120 символов>
---

# TestFlight Shipper

`xcodebuild archive` → `xcodebuild -exportArchive` → `xcrun altool --upload-app`.
Требует provisioning profile + Apple ID credential в keychain — НЕ передавай
секреты в prompt. Если fail — verdict=failed, one_line с классификацией ошибки.
