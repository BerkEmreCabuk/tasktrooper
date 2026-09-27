---
key: notices.pipeline_bounce_comment
version: 1
inputs: [SHA]
---
Pipeline aynı commit için yine kırmızı ({{.SHA}}) ve arada YENİ bir commit gelmedi. Bu görev bu commit yüzünden zaten bir kez geri gönderildi; sonuç değişmediği için board onu tekrar döngüye sokmayacak — kart `blocked` kolonuna alındı.

Yapılması gereken bir insanda: CI'ı düzeltin (build hatası, ya da hesap/faturalandırma kaynaklı olarak "job was not started" diyen bir Actions çalıştırması) ve ardından kartı elle ilerletin. Ajan çalıştırmak bu noktada aynı sonucu üretir ve kotayı harcar.
