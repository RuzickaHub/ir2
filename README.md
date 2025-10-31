# AI-Morph Gallery (Go)
Rozšířená verze galerie s:
- zjištěním rozlišení obrázků
- čtením EXIF metadat (pokud jsou přítomna)
- GitHub Actions workflow, který:
  - buildí Go binary
  - vytváří Docker image a pushuje do GHCR
  - publikuje statické soubory (templates + static) na gh-pages branch

## Spuštění lokálně
```
go mod tidy
go run main.go
```

Server poběží na `http://localhost:8080`.

## Poznámky k workflow
Workflow použije `GITHUB_TOKEN` a ghcr pro push Docker image. Pro push na GHCR doporučujeme povolit pakování a přístup (GHCR používá `GITHUB_TOKEN`).
