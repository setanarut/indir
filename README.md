# indir

Go ile yazılmış çok bağlantılı HTTP dosya indirici paketi.

## Özellikler

- **`-s` (segments)** — dosyayı N parçaya böler, her parça ayrı goroutine'de indirilir  
- **`-x` (connections)** — aynı anda maksimum N bağlantı kullanır  
- **Resume** — Ctrl-C ile duraklatınca durum `.dstate` dosyasına kaydedilir; aynı komutla kaldığı yerden devam eder  
- **Progress** — yüzde, hız (EMA), kalan süre (ETA) tek satırda gösterilir  
- **Range yoksa tek bağlantı** — sunucu `Accept-Ranges` başlığı göndermiyorsa otomatik fallback  

## Paket olarak kullanım

```go
import "github.com/setanarut/indir"

d := indir.New(indir.Config{
    URL:            "https://example.com/file.iso",
    OutputPath:     "file.iso",
    MaxConnections: 4,  // -x 4
    Segments:       8,  // -s 8
})
if err := d.Download(); err != nil {
    log.Fatal(err)
}
```

## Dosya yapısı

```
indir/
├── indir.go        # Config, Downloader, Download(), HTTP mantığı
├── segment.go      # segment tipi, splitSegments()
├── state.go        # JSON kayıt/yükleme (.dstate dosyası)
├── progress.go     # terminal ilerleme çubuğu, formatlayıcılar
└── go.mod
```

## Nasıl çalışır

### Parçalama

```
[0 ─────────── 249 MB] segment 0  ─┐
[250 ─────────── 499 MB] segment 1  ├─ eşzamanlı
[500 ─────────── 749 MB] segment 2  ┤
[750 ─────────── 999 MB] segment 3  ─┘

Tamamlandığında → file.iso olarak birleştirilir
```

### Resume

Her segment `.file.iso.part0`, `.file.iso.part1`, … temp dosyasına indirilir.  
Duraklatıldığında `.file.iso.dstate` JSON dosyasına her segmentin ne kadar indirildiği kaydedilir.  
Tekrar başlatıldığında state dosyası okunur, disk üzerindeki temp dosya boyutları doğrulanır, eksik kısımlar `Range: bytes=N-M` başlığıyla indirilir.

### İlerleme çıktısı örneği

```
[████████████░░░░░░░░░░░░░░░░] 42.3%  422.34 MB / 1.00 GB  9.15 MB/s   ETA 1m1s
```
