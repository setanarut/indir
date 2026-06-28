package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/setanarut/indir"
)

func main() {
	x := flag.Int("x", 5, "sunucu başına maksimum bağlantı sayısı")
	s := flag.Int("s", 5, "dosyanın kaç parçaya bölüneceği")
	o := flag.String("o", "", "çıktı dosyası adı (varsayılan: URL'den türetilir)")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() < 1 {
		usage()
		os.Exit(1)
	}

	url := flag.Arg(0)

	// Çıktı adı verilmediyse URL'nin son parçasından türet
	out := *o
	if out == "" {
		out = filepath.Base(url)
		if out == "." || out == "/" || out == "" {
			out = "download"
		}
	}

	d := indir.New(indir.Config{
		URL:            url,
		OutputPath:     out,
		MaxConnections: *x,
		Segments:       *s,
	})

	if err := d.Download(); err != nil {
		log.Fatalf("hata: %v\n", err)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Kullanım: dl [seçenekler] <URL>\n\n")
	fmt.Fprintf(os.Stderr, "Seçenekler:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nÖrnekler:\n")
	fmt.Fprintf(os.Stderr, "  dl https://example.com/file.zip\n")
	fmt.Fprintf(os.Stderr, "  dl -x 8 -s 16 https://example.com/file.iso\n")
	fmt.Fprintf(os.Stderr, "  dl -o benim-dosyam.zip https://example.com/file.zip\n")
}
