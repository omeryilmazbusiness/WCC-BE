# WODI CRM — Go-Live Backlog (Monzer.pdf v1.0 %100 uyum)

Kaynak: 27.09.2026 PDF uyum + SOLID denetimi. Başlangıç: PDF kapsamı ~%58, canlıya hazırlık ~%35.
Hedef: PDF §1–§26 %100, OWASP ASVS L2, 8 kabul senaryosu gerçek backend üzerinde yeşil.

## Çalışma kuralları

- **Numaralandırma:** Epic 19–26, task T-236 → T-345 (Epic 18 T-235'te bitti).
- **Katman:** `BE` = wodi-crm-be (Go), `FE` = WCC-FE (Next.js), `OPS` = CI/altyapı.
- **Öncelik:** `P0` Kritik (canlı engeli) · `P1` Yüksek · `P2` Orta.
- **SP:** Fibonacci story point. Sprint = 2 hafta, varsayılan ekip 2 BE + 2 FE + 0.5 OPS (~40 BE SP, ~32 FE SP / sprint).
- **Definition of Ready:** PDF referansı, kabul kriterleri, bağımlılıklar net; migration gerekiyorsa şema taslağı ekli.
- **Definition of Done (her task):** kod + test (domain unit, repository integration, HTTP) · RBAC + şube/sahiplik kapsamı · audit (hassas işlemde) · i18n en/ar + RTL · `go build/vet/test`, `tsc`, `eslint` yeşil · README/Docs güncel · demo fallback yok.

---

## Epic 19 — Güvenlik temeli (P0) · PDF §3, §24

| ID | Katman | Öncelik | Başlık | Kabul kriterleri | PDF | SP | Bağımlı |
|---|---|---|---|---|---|---|---|
| T-236 | BE | P0 | Erişim kapsamı modeli | `authz.Scope{BranchIDs, UserID, Role, TeamIDs}` context'e middleware ile yazılır; domain `CanAccess(scope, branchID, ownerID)` saf fonksiyon; tablo testleri 6 rol × 3 senaryo | §24 | 3 | — |
| T-237 | BE | P0 | Şube filtreli repository (çekirdek) | booking, lead, customer `FindByID/Update/Transition` sorguları `WHERE id=$1 AND branch_id=$2`; başka şube → `NotFound` (varlık sızdırmaz) | §24 | 5 | T-236 |
| T-238 | BE | P0 | Şube filtreli repository (diğer) | document, visa, payment, task, revenue target, supplier, conversation, package/departure, rooming, notification, AI run aynı kurala geçer | §24 | 8 | T-237 |
| T-239 | BE | P0 | Servis katmanı kapsam aktarımı | Tüm app servisleri `Scope` alır; handler'lar `claims`'ten türetir; hiçbir servis kapsamsız `Get` çağıramaz (lint testi) | §24 | 5 | T-238 |
| T-240 | BE | P0 | Sahiplik + takım kapsamı | Employee: lead/booking/task/conversation listelerinde `owner_id=self` zorunlu, `OwnsOrElevated` tüm id'li uçlarda; Manager: `team_members` tablosu (migration) ile takım; GM/Admin: çoklu şube seçimi | §3, §24 | 8 | T-239 |
| T-241 | BE | P0 | IDOR regresyon test paketi | Router walker tüm `/{id}` uçlarını 2 şube × 6 rol ile çağırır; yetkisiz her istek 403/404; CI'da zorunlu | §24, §26 | 5 | T-240 |
| T-242 | BE | P0 | `/customers` RBAC | `customers.read` / `customers.write` izinleri rbac matrisine; 11 route guard'lı; `/dashboard/my-target` guard'lı; "her mutasyon route'u guard'lı" route testi | §3 | 3 | — |
| T-243 | BE | P0 | Alan bazlı redaksiyon | `cost_amt`, `margin` yalnız `payments.read`/finance rollerine; pasaport yalnız `pii.read` ile tam, diğerlerine maskeli (participants dahil) | §10, §24 | 3 | T-242 |
| T-244 | FE | P0 | Sessiz demo fallback kaldırma | `shared/api/createRepository` tek fabrika; memory repo yalnız `NEXT_PUBLIC_DEMO_MODE=true` **ve** ağ hatası + okuma; yazmalarda ve 4xx/5xx'te asla; 21 entity repo geçer | §24 | 8 | — |
| T-245 | FE | P0 | Hata UX standardı | Tipli `ApiError`: 401→refresh/login, 403→`PermissionDenied`, 409 çakışma, 422 alan hataları, 5xx toast + retry; tüm widget'lar `QueryState`; mutasyonlarda başarı/başarısızlık toast'u | §24 | 5 | T-244 |
| T-246 | BE | P0 | `/v1/me` izin listesi | Kullanıcı, rol, şubeler, takım ve efektif izin listesi döner; rbac matrisinden türetilir | §3 | 2 | T-242 |
| T-247 | FE | P0 | Rol-doğru menü ve buton kapısı | Nav, `middleware.ts` ve aksiyon butonları `/v1/me` izinlerinden; finance/ops/admin backend'in yasakladığı ekranı görmez; `<Can perm>` bileşeni | §3 | 5 | T-246 |
| T-248 | BE | P0 | Webhook imza doğrulama | Meta `X-Hub-Signature-256`, Gmail Pub/Sub JWT, generic HMAC; zaman damgası replay penceresi 5 dk; geçersiz → 401 + audit | §7, §24 | 5 | — |
| T-249 | BE | P0 | Şubeyi entegrasyon hesabından çözme | `(provider, external_account_id) → integration_accounts.branch_id`; `branch_id` query param kaldırılır; unique index migration | §7 | 3 | T-248 |
| T-250 | BE | P0 | Webhook giriş sertleştirme | Body limit, IP rate limit, ham olay `webhook_events` (received/processed/failed) tablosuna; idempotent işleme | §7, §23 | 3 | T-249 |
| T-251 | BE | P0 | Gerçek TOTP MFA | RFC 6238; `mfa_secret_enc` şifreli; enroll (otpauth URL), verify, 10 hash'li kurtarma kodu, parola ile kapatma; `InMemoryMFA` prod wiring'den çıkar | §24 | 5 | T-258 |
| T-252 | FE | P0 | MFA ekranları | Profilde QR ile kayıt + kod onayı + kurtarma kodu indirme; login'de MFA adımı; kapatma | §24 | 5 | T-251 |
| T-253 | BE | P0 | Login hız sınırı + kilitleme | Redis sliding window (IP+email); 5 hatada 15 dk kilit, artan süre; audit; admin unlock endpoint | §24 | 3 | — |
| T-254 | FE | P0 | Kilit mesajları + admin unlock | Login'de kalan süre mesajı; kullanıcı yönetiminde "Kilidi aç" | §24 | 2 | T-253 |
| T-255 | BE | P0 | Refresh token rotasyonu | `refresh_tokens` tablosu, aile bazlı yeniden kullanım tespiti, logout iptal eder, parola değişince tümü iptal | §24 | 5 | — |
| T-256 | FE | P0 | HttpOnly oturum (BFF) | Next route handler'ları `/api/auth/{login,refresh,logout}` HttpOnly+Secure+SameSite=Lax cookie; API çağrıları BFF proxy; `document.cookie` yazımı kaldırılır | §24 | 5 | T-255 |
| T-257 | BE | P0 | HTTP sertleştirme | Güvenlik başlıkları, CORS allowlist, istek boyutu limiti, güvenilir proxy'den gerçek IP | §24 | 2 | — |

## Epic 20 — Veri koruma & audit (P1) · PDF §20, §24

| ID | Katman | Öncelik | Başlık | Kabul kriterleri | PDF | SP | Bağımlı |
|---|---|---|---|---|---|---|---|
| T-258 | BE | P1 | AES-GCM şifreleme platformu | `platform/crypto` envelope şifreleme, anahtar env/KMS, key-id versiyonlu rotasyon; unit test | §24 | 3 | — |
| T-259 | BE | P1 | AI ve kanal anahtarları şifreli | `config_json` sırları `secrets_enc` kolonuna; backfill migration; API asla düz döndürmez | §18, §24 | 3 | T-258 |
| T-260 | BE | P1 | Pasaport şifreleme | customers + participants: `passport_enc`, HMAC blind index `passport_hash` (dedupe/arama), `passport_last4`; backfill; düz kolon kaldırılır | §9, §24 | 5 | T-258 |
| T-261 | BE | P1 | PII görüntüleme audit'i | `POST /customers/{id}/reveal-passport` (`pii.read`) + `pii.revealed` audit | §24 | 2 | T-260 |
| T-262 | FE | P1 | Maskeli pasaport + "Göster" | Tüm ekranlarda maskeli; izinliyse göster butonu | §9 | 2 | T-261 |
| T-263 | BE | P1 | Audit düzeltme | Actor daima context'ten; before/after: booking status/update/participant/line item/discount/readiness override, payment refund/reverse/adjust, capacity, doküman inceleme, hedef revizyonu, ayar değişiklikleri; branch_id, IP, UA, session_id | §20 | 8 | T-236 |
| T-264 | BE | P1 | Append-only audit | Hassas işlemlerde audit aynı transaction'da (fail-closed); `audit_events` UPDATE/DELETE engelleyen trigger + REVOKE | §20 | 3 | T-263 |
| T-265 | BE | P1 | Değişmez ödeme defteri | Trigger: yalnız izinli status geçişleri; tutar/para birimi güncellenemez | §14 | 2 | — |
| T-266 | FE | P1 | Audit görüntüleyici | Before/after diff, actor, IP, session; entity/actor/tarih filtreleri; CSV export | §20 | 3 | T-263 |
| T-267 | BE | P2 | KVKK/GDPR işlemleri | Müşteri veri dışa aktarma + anonimleştirme uçları (izin + audit) | §24 | 3 | T-260 |

## Epic 21 — Rezervasyon yaşam döngüsü & finans doğruluğu (P1) · PDF §10, §14

| ID | Katman | Öncelik | Başlık | Kabul kriterleri | PDF | SP | Bağımlı |
|---|---|---|---|---|---|---|---|
| T-268 | BE | P1 | 9 durumlu rezervasyon | Migration CHECK: draft, quoted, option_hold, confirmed, partially_paid, ready, travelled, completed, cancelled + veri taşıma; domain durum makinesi ve guard'lar; tüm geçişler tablo testiyle | §10 | 8 | T-263 |
| T-269 | BE | P1 | Otomatik geçişler | Ödeme → partially_paid; hazırlık şartları → ready; kalkış tarihi → travelled; hold süresi dolunca → cancelled/draft (scheduler) | §10, §22 | 5 | T-268, T-279 |
| T-270 | FE | P1 | Durum UI | 9 durum çipi, duruma göre geçiş butonları, hold geri sayımı, manager override dialogu (gerekçe zorunlu) | §10 | 5 | T-268 |
| T-271 | BE | P1 | Override yetkisi | `bookings.override` izni (manager/gm); gerekçe + audit | §10 | 1 | T-268 |
| T-272 | BE | P1 | Kur tablosu ve gerçek çevrim | `fx_rates(base, quote, rate, effective_date, source)`; tarih bazlı `Convert`; admin CRUD + opsiyonel sağlayıcı job; payment/booking'de `amount_reporting` snapshot | §14 | 5 | — |
| T-273 | FE | P1 | Kur yönetimi ekranı | Kur listesi/ekleme; finans ekranlarında çift para birimi | §14 | 3 | T-272 |
| T-274 | BE | P1 | Rezervasyon finans bileşenleri | Vergi/ücret satırları, `bookings.discount` izinli indirim, ödeme sözü (`payment_promises` + görev); özet discount/cost/tax içerir | §14 | 5 | T-268 |
| T-275 | FE | P1 | Finans paneli tamamlama | Bileşen dökümü, indirim düzenleme (izinli), ödeme sözü oluşturma/liste | §14 | 3 | T-274 |
| T-276 | BE | P1 | Görevler ayrılığı (SoD) | İade talep eden ≠ onaylayan; `auto_verify` yalnız `payments.approve`; `received_at` alanı | §14 | 2 | — |
| T-277 | BE | P2 | Yan etkisiz kuyruk okuma | Overdue kuyruğu okumak DB değiştirmez; durum güncellemesi scheduler job'ında | §14 | 1 | T-279 |
| T-278 | BE | P2 | Finans export BOM + audit | Kuyruk CSV'leri UTF-8 BOM + export audit | §14, §20 | 1 | — |

## Epic 22 — Otomasyon: scheduler, worker, olaylar, bildirimler (P1) · PDF §12, §19, §22, §23

| ID | Katman | Öncelik | Başlık | Kabul kriterleri | PDF | SP | Bağımlı |
|---|---|---|---|---|---|---|---|
| T-279 | BE | P1 | asynq.Scheduler | `cmd/worker` cron tablosu (config): SLA 1dk, eskalasyon 5dk, görev gecikme 5dk, ödeme hatırlatma saatlik, doküman/pasaport bitiş günlük, tedarikçi onay günlük, AI günlük özet 07:00 şube saat dilimi, hedef recompute saatlik, hold expiry 5dk, travelled günlük, planlı raporlar; prod'da Redis yoksa readiness fail (memory fallback yok) | §22 | 5 | — |
| T-280 | BE | P1 | Worker handler'ları gerçek iş | reminder, report, ai_summary, webhook_retry, supplier confirm (görev), doküman bitiş öncesi 30/14/7 gün, pasaport bitiş | §13, §17, §18 | 8 | T-279 |
| T-281 | BE | P1 | Transactional outbox | `outbox` tablosu, iş ile aynı tx'te yazım, dispatcher backoff + dead-letter; kritik olaylar dayanıklı | §23 | 8 | — |
| T-282 | BE | P1 | Olay kataloğu tamam | conversation.message_received, conversation.responded, lead.stage_changed, payment.reversed, document.status_changed, task.overdue, target.status_changed, integration.failed, import.completed yayınlanır; "katalogdaki her olay yayınlanıyor" testi | §23 | 5 | T-281 |
| T-283 | BE | P1 | Abonelikler | payment.reversed → hedef + booking recompute; document.status_changed → hazırlık + görev kapatma; conversation.responded → SLA stop + görev kapatma; target.status_changed → bildirim + kurtarma görevi | §22, §23 | 5 | T-282 |
| T-284 | BE | P1 | Eksik 5 bildirim tetikleyicisi | Ödeme vadesi, eksik doküman, hedef geride (`CheckBehindAlerts` planlı), entegrasyon hatası, sonraki aksiyonu olmayan lead; SLA A (uyarı) / B (ihlal) eşikleri `alert_threshold_settings`'ten | §19 | 5 | T-282 |
| T-285 | BE | P1 | Eskalasyon motoru DB kurallarını kullanır | `ProcessEscalations` `MergeEscalation` sonucunu kullanır; kural bazlı grace | §19 | 2 | T-279 |
| T-286 | BE | P1 | Otomatik görevler tamam | Cevapsız mesaj, vize takip, tedarikçi onayı, eksik doküman kural görevleri; `tasks.source_rule`, `created_by` kolonları; doküman/vize/inbox çözülünce otomatik kapanış | §12 | 5 | T-283 |
| T-287 | FE | P1 | Bildirim merkezi | Gruplu bildirimden filtreli listeye inme, onayla/çöz, SSE ile anlık | §19 | 3 | T-288 |
| T-288 | BE | P1 | SSE akışı | `/v1/stream` kullanıcı kapsamlı bildirim + dashboard invalidation olayları | §5, §19 | 3 | T-282 |
| T-289 | FE | P2 | Eşik/eskalasyon admin | A/B SLA, kural aç/kapa gerçek motora bağlı | §19 | 2 | T-285 |

## Epic 23 — Canlı kanal ve dosya entegrasyonları (P1) · PDF §7, §16

| ID | Katman | Öncelik | Başlık | Kabul kriterleri | PDF | SP | Bağımlı |
|---|---|---|---|---|---|---|---|
| T-290 | BE | P1 | WhatsApp Cloud API | Gerçek gönderim (metin, şablon, medya), status webhook (sent/delivered/read/failed), 24 saat penceresi dışı şablon zorunlu | §7 | 8 | T-248 |
| T-291 | BE | P1 | Instagram + Messenger | Graph API gönder/al, ekler | §7 | 5 | T-248 |
| T-292 | BE | P1 | E-posta | Gmail API (OAuth2) + SMTP/IMAP; Message-ID ile thread | §7 | 8 | T-248 |
| T-293 | BE | P1 | Giden mesaj dayanıklılığı | Exponential backoff + dead-letter + integration.failed; her çağrı `integration_logs`'a yazılır | §7, §20 | 3 | T-281 |
| T-294 | BE | P1 | Gerçek sağlık kontrolü | Token geçerlilik ping'i; stub'lar "stub" döner, yalnız dev'de yüklenir | §7 | 2 | — |
| T-295 | FE | P1 | Kanal bağlama akışları | Meta embedded signup, Google OAuth, sağlık rozetleri, entegrasyon log ekranı | §7 | 5 | T-290 |
| T-296 | BE | P2 | OneDrive/SharePoint gerçek adaptör | Graph OAuth, delta sync, xlsx çekme → import pipeline | §16 | 8 | T-318 |
| T-297 | BE | P1 | Mesaj ekleri | Gelen medya → MinIO document + `message.document_id`; giden ek | §7 | 3 | T-290 |

## Epic 24 — Ekran tamamlama (P1/P2) · PDF §5–§20

| ID | Katman | Öncelik | Başlık | Kabul kriterleri | PDF | SP | Bağımlı |
|---|---|---|---|---|---|---|---|
| T-298 | BE | P1 | Manager dashboard sorguları | period=today/week/month/season/year/custom; branch (GM çoklu), team, employee, package, channel filtreleri; KPI: yeni lead, cevapsız konuşma, bekleyen rezervasyon, ödeme vadesi; hedef variance + forecast; takım tablosu SLA, rezervasyon, tahsilat | §5 | 8 | T-240 |
| T-299 | FE | P1 | Manager dashboard | Filtre çubuğu, periyot seçici, 8 tıklanabilir KPI → filtreli liste (URL query), hero expected/variance/forecast, gruplu uyarılar kayda link, hızlı aksiyonlar dialog açar (rezervasyon, görev, Excel), SSE auto-refresh | §5 | 8 | T-298, T-288 |
| T-300 | BE | P1 | Employee home verileri | My-work'e okunmamış konuşma, bekleyen doküman/ödeme; aşama sayıları; son müşteri/rezervasyon ucu | §6 | 3 | T-240 |
| T-301 | FE | P1 | Employee home | Aşama sayıları, hedef açığı, son kayıtlar, hızlı kayıt (lead/rezervasyon/not/yükleme), "sıradaki", outcome + sonraki aksiyon girişi | §6 | 5 | T-300 |
| T-302 | BE | P1 | Inbox eksikleri | Okunmamış kuyruk (kullanıcı bazlı okundu), atanabilir kullanıcı ucu, manuel müşteri eşle/oluştur, konuşmaya bağlı lead (`conversation.lead_id`), konuşmadan rezervasyon, değişkenli şablon render | §7 | 5 | T-240 |
| T-303 | FE | P1 | Inbox tamamlama | Okunmamış kuyruk, zaman damgası, kanal rozeti, ek görüntüle/yükle, şablon seçici, sağ panel (müşteri, lead aşaması, rezervasyon, görev, iç not), gerçek atama listesi; inbox-board alt widget'lara bölünür | §7 | 8 | T-302 |
| T-304 | BE | P1 | CRM lead modeli | waiting + booked aşamaları (migration + geçmiş eşleme); package_interest_id, value_estimate, last_contact_at, next_action(+_at); kayıp nedeni `lost_reason_codes`'tan; source/package/tarih/aşama/kayıp filtreleri; convert konuşma bağını korur | §8 | 5 | — |
| T-305 | FE | P1 | Pipeline tamamlama | Waiting/Booked kolonları, kart alanları + SLA/risk rozeti, filtreler, API'den kayıp nedenleri, dönüşüm analitiği ekranı, AI öncelik açıklama paneli | §8 | 5 | T-304 |
| T-306 | BE | P1 | Customer 360 verisi | `customer_phones`, `passport_expiry`, preferences düzenleme, update'te duplicate kontrol, merge görev/doküman/konuşma/ödeme taşır, timeline konuşma içerir, AI müşteri özeti | §9 | 5 | T-260 |
| T-307 | FE | P1 | Customer 360 | Lead/rezervasyon/görev oluştur, konuşma sekmesi, tercih editörü, düzenlemede duplicate uyarısı, pasaport bitiş rozeti, AI özet kartı, sekme aksiyonları | §9 | 5 | T-306 |
| T-308 | BE | P1 | Booking workspace verisi | Detayda müşteri adı, departure, owner, paid; aktivite timeline ucu; rezervasyon bazlı tedarikçi onayları; müşteri/inbox'tan oluşturma | §10 | 5 | T-268 |
| T-309 | FE | P1 | Booking workspace | Tam header, timeline, bağlı konuşma, tedarikçi onay sekmesi, indirim düzenleme; booking-detail-board bölünür | §10 | 5 | T-308 |
| T-310 | BE | P2 | Paket/departure tamamlama | Otel/transfer/grup lideri alanları, maliyet tahmini + hedef marj, departure karlılık ucu, kapasite değişikliği audit, departure rezervasyon listesi | §11 | 5 | T-263 |
| T-311 | FE | P2 | Departure detay sayfası | Rezervasyon listesi, hazırlık, kapasite düzenleme, otel/transfer/lider, karlılık görünümü | §11 | 5 | T-310 |
| T-312 | BE | P2 | Görev tamamlama | Yorumlar, yeniden atama talebi/onayı, basit tekrar (günlük/haftalık), kontrollü outcome kodları, creator | §12 | 5 | T-286 |
| T-313 | FE | P2 | Görev çekmecesi | Yorumlar, atama talebi, tekrar, outcome seçimi | §12 | 3 | T-312 |
| T-314 | BE | P2 | Doküman tamamlama | Paket/uyruk bazlı politika eşleme, `document_events` geçmişi, bitiş öncesi hatırlatma, OCR confirm-before-save ucu | §13 | 3 | T-280 |
| T-315 | FE | P2 | Doküman UI | Politika editörü (paket/uyruk), sürüm/değiştirme geçmişi, OCR onay akışı | §13 | 5 | T-314 |
| T-316 | BE | P1 | Hedef motoru tamamlama | Company scope, takım filtresi, para birimi filtresi, variance %, kalan tutar, eşikler ayarlardan, ödemelerden günlük gerçek seri, geride kalınca kurtarma görevi | §15 | 5 | T-284 |
| T-317 | FE | P1 | Hedef UI | Tam oluşturma formu (scope, metrik, para birimi, dağılım), aylık mevsimsellik editörü (toplam ≠ %100 ise kayıt engeli), pay atama ekranı, gerçek trend grafiği | §15 | 5 | T-316 |
| T-318 | BE | P1 | Excel içe aktarım tamamlama | Booking/payment/departure yazıcıları, sayfa listeleme/seçme, mükerrer tespit adımı, gerçek rollback (`import_job_rows` + pencere), async işleme + import.completed bildirimi | §16 | 8 | T-282 |
| T-319 | FE | P1 | Excel sihirbazı | Sayfa seçimi, mükerrer inceleme, şablon kaydet/uygula, rollback, ilerleme, export kolon seçici | §16 | 5 | T-318 |
| T-320 | BE | P2 | Tedarikçi tamamlama | Kategori, rezervasyon bağları, AI tedarikçi özeti | §17 | 3 | — |
| T-321 | FE | P2 | Tedarikçi UI | Kategori alan/filtre, bağlı rezervasyonlar, AI özet kartı | §17 | 2 | T-320 |
| T-322 | BE | P1 | AI yetki + anomali | Employee için sahiplik kapsamlı AI bağlamı, kapsamlı feedback ucu, deterministik anomali kuralları (tahsilat düşüşü, iptal artışı, SLA bozulması) + AI anlatım | §18 | 5 | T-240 |
| T-323 | FE | P1 | AI UI | AI çıktılarında feedback, dashboard anomali kartı, özet formatı "Ne değişti / Neden önemli / Önerilen aksiyon" | §18 | 3 | T-322 |
| T-324 | BE | P2 | Rapor tamamlama | Entegrasyon log raporu dolu, audit raporu IP/session, planlı raporlar (e-posta CSV) | §20 | 3 | T-293 |
| T-325 | FE | P2 | Rapor UI | Rapor planlama, entegrasyon log sekmesi | §20 | 2 | T-324 |
| T-326 | BE | P2 | Arapça arama | Normalizasyon (elif/hemze, ta marbuta, harekeler, tatvil) fonksiyonu + normalize kolonlar + trigram index; pasaport blind index araması | §24 | 3 | T-260 |
| T-327 | FE | P2 | i18n/RTL son | `<html lang dir>` locale'e göre, Arapça arama UX, sabit metin taraması | §24 | 2 | — |

## Epic 25 — Mimari refactor (P2) · SOLID / FSD

| ID | Katman | Öncelik | Başlık | Kabul kriterleri | PDF | SP | Bağımlı |
|---|---|---|---|---|---|---|---|
| T-328 | BE | P2 | ISP: repository port bölme | supplier, payment, inbox, booking, revenuetarget, adminconfig, customer → Reader/Writer/Queries portları (≤7 metot) | §2 | 8 | T-239 |
| T-329 | BE | P2 | Constructor injection | `Set*` kaldırılır; wire.go modül bazlı `Wire*` fonksiyonlarına bölünür; eksik bağımlılık açılışta hata | §2 | 5 | T-328 |
| T-330 | BE | P2 | DIP + demo owner | `events.Publisher` arayüzü; handler'lar arayüze bağlı; sabit demo owner yerine şube bazlı round-robin atama politikası | §2, §7 | 3 | T-281 |
| T-331 | BE | P2 | Gözlemlenebilirlik | slog JSON istek logu (request_id, user, branch), OpenTelemetry trace, Prometheus metrik, alarm kuralları | §24 | 3 | — |
| T-332 | FE | P2 | FSD temizliği | Tek `shared/api` client (tokenFromCookie tekil), 15 derin import public API'ye, feature→feature importları widget'a taşınır | §2 | 3 | T-244 |
| T-333 | FE | P2 | Büyük widget bölme | inbox, import-export, target, booking detail, booking ops, admin settings ≤300 satır bileşenlere | §2 | 5 | T-303, T-309 |
| T-334 | BE | P2 | OpenAPI sözleşmesi | OpenAPI 3 spec + FE tipli client üretimi; CI'da sözleşme diff kontrolü | §23 | 5 | — |

## Epic 26 — Kalite, DoD ve canlıya çıkış (P0/P1) · PDF §24–§26

| ID | Katman | Öncelik | Başlık | Kabul kriterleri | PDF | SP | Bağımlı |
|---|---|---|---|---|---|---|---|
| T-335 | BE | P1 | Testcontainers altyapısı | Postgres + Redis + MinIO container, migration, seed factory'leri | §26 | 3 | — |
| T-336 | BE | P1 | Repository entegrasyon testleri | Tüm postgres paketleri, şube kapsamı dahil | §26 | 8 | T-335, T-238 |
| T-337 | BE | P1 | Servis unit testleri | Testsiz 13 app paketi (payment, ai, auth, audit, notification, report, visa, supplier, useradmin, extint, filesync, rooming, search) | §26 | 8 | — |
| T-338 | BE | P0 | Uçtan uca WF-01..05 | HTTP router üzerinden: webhook → lead → convert → booking → payment → doküman → ready → hedef → rapor; PDF'teki 8 kabul senaryosu | §22, §26 | 8 | T-335, Epic 21–22 |
| T-339 | FE | P0 | Playwright gerçek backend | docker compose ile gerçek BE, demo mode kapalı; 6 rol × 8 kabul senaryosu, RTL | §26 | 8 | T-244, T-338 |
| T-340 | FE | P1 | FE unit testleri | Vitest: para formatı, izin kapısı, repo hata davranışı | §26 | 3 | T-244 |
| T-341 | OPS | P0 | CI/CD pipeline | Lint, test, e2e, migration kontrolü, image build, SAST, bağımlılık ve secret taraması | §24 | 5 | — |
| T-342 | OPS | P0 | Ortamlar ve sırlar | Staging/prod, Vault/KMS, yedekten geri dönüş tatbikatı yapılmış, migration geri dönüş planı | §24 | 5 | T-258 |
| T-343 | OPS | P1 | Performans | k6 yük testi (PDF NFR p95 hedefleri), eksik index'ler (`bookings(owner_id)` vb.), sorgu planları | §24 | 3 | T-298 |
| T-344 | OPS | P0 | Güvenlik testi | OWASP ASVS L2 checklist + pentest, bulguların kapatılması | §24 | 5 | Epic 19–20 |
| T-345 | OPS | P0 | Canlıya çıkış | Runbook, izleme alarmları, 6 rol UAT onayı, Excel'den veri taşıma, hypercare planı | §25, §26 | 3 | Tümü |

---

## Sprint planı (sıralı)

| Sprint | Hedef | BE | FE / OPS |
|---|---|---|---|
| S1 | Güvenlik çekirdeği | T-236, T-237, T-238, T-242, T-243, T-246, T-257 | T-244, T-245, T-341 |
| S2 | Kapsam + kimlik | T-239, T-240, T-241, T-248, T-249, T-250, T-253 | T-247, T-254 |
| S3 | Kimlik + şifreleme + audit | T-258, T-251, T-255, T-259, T-260, T-261, T-263 | T-252, T-256, T-262 |
| S4 | Audit + rezervasyon + otomasyon altyapısı | T-264, T-265, T-268, T-271, T-276, T-279, T-281 | T-266, T-270, T-342 |
| S5 | Olaylar + finans | T-269, T-272, T-274, T-277, T-278, T-282, T-283, T-284 | T-273, T-275 |
| S6 | Worker + bildirim + kanallar | T-280, T-285, T-286, T-288, T-290, T-293, T-294 | T-287, T-289, T-295 |
| S7 | Kanallar + dashboard + inbox | T-291, T-292, T-297, T-298, T-300, T-302 | T-299, T-301, T-303 |
| S8 | CRM + müşteri + booking + hedef + excel | T-304, T-306, T-308, T-316, T-318, T-322 | T-305, T-307, T-309, T-317 |
| S9 | Kalan ekranlar + test altyapısı | T-310, T-312, T-314, T-320, T-324, T-326, T-335, T-337 | T-311, T-313, T-315, T-319, T-321, T-323, T-325, T-327 |
| S10 | Refactor + entegrasyon testleri | T-296, T-328, T-329, T-330, T-331, T-334, T-336 | T-332, T-333, T-340 |
| S11 | DoD + canlı | T-267, T-338 | T-339, T-343, T-344, T-345 |

**Kritik yol:** T-236 → T-240 → T-263 → T-268 → T-281/T-282 → T-284 → T-338 → T-339 → T-344 → T-345.

**Kilometre taşları:**
- S2 sonu: şubeler arası erişim kapalı, sahte veri yok, rol-doğru menü (demo güvenli).
- S4 sonu: MFA, şifreleme, append-only audit (pilot müşteriye açılabilir).
- S7 sonu: canlı kanallar, scheduler, tüm olaylar ve bildirimler (ürün tamam, beta).
- S11 sonu: PDF %100, 8 kabul senaryosu yeşil, pentest temiz → **Go-live**.

## Riskler

| Risk | Etki | Önlem |
|---|---|---|
| Meta/Google uygulama onay süreleri | S6–S7 kanal işleri gecikir | S1'de başvuru; stub ile paralel geliştirme |
| Pasaport/sır backfill migration'ı | Veri kaybı | Staging'de prod kopyasıyla prova, geri dönüş scripti |
| 9 durumlu booking migration'ı | Mevcut kayıtların yanlış eşlenmesi | Eşleme tablosu + dry-run raporu |
| Kapsam değişikliğinde regresyon | Rollerde veri kaybolur/görünür | T-241 IDOR paketi her PR'da zorunlu |
