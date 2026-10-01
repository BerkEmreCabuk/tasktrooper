---
key: guard.handoff_unseen_ui
version: 1
---
Otomatik code_review geçişi yapılmadı: bu run arayüzü değiştirdi ama ekrana hiç bakmadı (browser_screenshot / browser_read_dom / mobile_screenshot / mobile_read_ui kaydı yok). Yeşil build ekranın doğru göründüğünü söylemez — eksik ikon "?" olarak render edilir, taşan bir öğe telefonda yatay kaydırma yapar, ikisi de derlenir. Bir sonraki run, web reposunda dev server'ı arka planda başlatıp değişen sayfayı açmalı, masaüstü ve mobil boyutta ekran görüntüsü almalı ve eklediği öğenin DOM'da olduğunu doğrulamalı. Mobil reposunda: kendi derlediğin build'i mobile_screenshot ile çek, ya da widget önizleyicisinin/simülatörün ekran görüntüsünü tarayıcı araçlarıyla aç.
