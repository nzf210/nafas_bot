# NAFAS ROADMAP: Menuju Produksi (v0.1.0)

Roadmap ini menyatukan persepsi kita dari Hari ke-1 hingga sistem berjalan di Production. Kita membangun *Modular Monolith* (rekomendasi: Golang) dengan arsitektur *Multi-Agent AI* dan antarmuka Telegram.

## Fase 1: Fondasi & Infrastruktur (Minggu 1)
**Tujuan:** Menyiapkan kerangka kerja dasar, database, dan struktur proyek.
*   **Hari 1-2:** Inisialisasi proyek, konfigurasi environment (Docker Compose, `.env`), setup struktur direktori (modular monolith: `internal/auth`, `internal/db`, dll).
*   **Hari 3-4:** Setup PostgreSQL schema dasar (tabel user, API keys terenkripsi, log sistem).
*   **Hari 5:** Setup Message Broker (Redis/NATS) untuk komunikasi internal asinkron (misal: webhook Telegram ke bot processor).
*   **Hari 6-7:** Setup framework logging terpusat dan penanganan error (sangat krusial untuk trading bot).

## Fase 2: Integrasi Exchange & Data (Minggu 2)
**Tujuan:** Sistem bisa membaca data pasar dan saldo akun tanpa melakukan trading.
*   **Hari 8-10:** Integrasi API Exchange (Binance/Bybit). Buat wrapper/interface yang seragam. Implementasi autentikasi API Key terenkripsi AES-256.
*   **Hari 11-12:** Modul Scanner (Data Ingestion). Mengambil data OHLCV, orderbook, dan ticker untuk BTC, ETH, SOL.
*   **Hari 13-14:** Sistem penyimpanan data market ke Postgres (untuk keperluan history & analisis AI) dan Redis (untuk data real-time).

## Fase 3: Antarmuka Telegram (Minggu 3)
**Tujuan:** Interaksi dengan user sudah bisa dilakukan sepenuhnya via Telegram.
*   **Hari 15-16:** Setup Telegram Bot API (long polling atau webhook).
*   **Hari 17-18:** Implementasi command dasar: `/dashboard` (status bot), `/portfolio` (saldo dari exchange).
*   **Hari 19-21:** Implementasi command manajemen: `/settings` (update API key, set risk tolerance), `/positions` (posisi aktif).

## Fase 4: Kerangka Kerja AI (Minggu 4 - 5)
**Tujuan:** Menghidupkan agen-agen AI dan merajut komunikasinya tanpa eksekusi real.
*   **Hari 22-24:** Integrasi LLM API (OpenAI/Anthropic) dan setup prompt/system message untuk masing-masing agen. Memastikan output selalu dalam format JSON.
*   **Hari 25-27:** Implementasi **AI Coordinator** (Orchestrator utama) & **Market Analyst** (menentukan bull/bear/crab).
*   **Hari 28-30:** Implementasi **Pair Analyst** (mencari setup trading) & **Trade Reviewer** (validasi logika setup).
*   **Hari 31-35:** Testing komunikasi antar agen (Coordinator -> Market -> Pair -> Reviewer) dengan data dummy.

## Fase 5: Mesin Risiko & Pembelajaran (Minggu 6)
**Tujuan:** Menambahkan lapis pengamanan utama dan memori AI.
*   **Hari 36-39:** Implementasi **Risk Guardian**. Ini adalah kode *hardcoded* (bukan hanya AI prompt) yang mengecek max exposure, wajib ada stop-loss, dan ukuran posisi (position sizing).
*   **Hari 40-42:** Desain tabel `trade_memories` dan `market_memories`. Implementasi awal **Learning Engine** untuk menyimpan log hasil keputusan agen.

## Fase 6: Eksekusi & Paper Trading (Minggu 7 - 8)
**Tujuan:** Menjalankan pipeline lengkap namun tanpa uang beneran (atau via Testnet).
*   **Hari 43-45:** Implementasi **Execution Advisor** (mengubah setup menjadi order API: Market/Limit/TWAP).
*   **Hari 46-50:** Menghubungkan seluruh pipeline: *Scanner -> AI -> Risk -> Execution*. Menjalankan bot di mode **Paper Trading** (Testnet Exchange).
*   **Hari 51-56:** Observasi Paper Trading. Evaluasi kualitas JSON dari AI, perbaiki prompt, pastikan Risk Guardian berhasil memblokir trade bodoh.

## Fase 7: Persiapan Produksi & Live (Minggu 9)
**Tujuan:** Memastikan keamanan, stabilitas, dan peluncuran V0.1.0.
*   **Hari 57-58:** Audit Keamanan. Cek enkripsi API key, validasi input Telegram, pastikan tidak ada secret bocor di log.
*   **Hari 59:** Setup VPS Production (contoh: DigitalOcean/AWS). Deploy via Docker Compose.
*   **Hari 60 (D-Day):** **Go Live**. Hubungkan API Key akun real (dengan dana kecil/sandbox limit dulu).

---

## Kesepakatan & Persepsi (Sesuai NAFAS_CONTRACT)
1.  **AI Tidak Klik Tombol Buy/Sell:** AI hanya menghasilkan *Rencana Eksekusi* (JSON). Modul Golang/Python yang akan mengeksekusi setelah lolos Risk Guardian.
2.  **Risk Guardian adalah Tembok:** Keputusan Risk Guardian bersifat absolut. Jika AI memaksa trade 50% modal, Risk Guardian yang ditulis dengan bahasa pemrograman (bukan AI) akan menolaknya.
3.  **Telegram-First:** Kita tidak butuh React/Vue/Web UI di V0.1.0. 100% interaksi via bot Telegram.
4.  **No HFT:** Bot ini pelan tapi pasti. Keputusan mungkin dibuat setiap jam atau 4 jam, bukan mili-detik.
