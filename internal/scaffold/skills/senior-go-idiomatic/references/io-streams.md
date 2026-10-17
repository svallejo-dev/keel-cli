# I/O, Streams & Buffers — Go Idiomatic Reference

## El Modelo Mental Correcto

`io.Reader` e `io.Writer` son **contratos**, no implementaciones. La potencia del modelo de I/O de Go está en que cualquier cosa que satisfaga ese contrato de un método puede ser compuesta con cualquier otra cosa que satisfaga el otro. `os.File`, `http.Response.Body`, `bytes.Buffer`, `gzip.Reader`, `tls.Conn` — todos hablan el mismo idioma.

```
io.Reader: Read(p []byte) (n int, err error)
io.Writer: Write(p []byte) (n int, err error)
```

Un solo método. Eso es todo. Y con eso, la stdlib entera construye pipelines de datos arbitrariamente complejos.

---

## io.Reader — El Contrato Completo

`Read` puede retornar en cuatro estados distintos. Ignorar cualquiera de ellos es un bug:

```go
n, err := r.Read(buf)
```

| `n` | `err` | Significado |
|---|---|---|
| `> 0` | `nil` | Datos leídos, más pueden seguir |
| `> 0` | `io.EOF` | Datos leídos, stream terminado — **procesa `n` bytes primero** |
| `0` | `io.EOF` | Stream terminado, sin datos nuevos |
| `0` | `!= nil` | Error, sin datos — el error es el mensaje |

El caso `n > 0, err == io.EOF` es el footgun más común: código que descarta `n` cuando ve `io.EOF` pierde la última porción de datos.

```go
// CORRECTO: procesar n antes de verificar err
func readAll(r io.Reader) ([]byte, error) {
    var result []byte
    buf := make([]byte, 4096)
    for {
        n, err := r.Read(buf)
        if n > 0 {
            result = append(result, buf[:n]...) // primero los datos
        }
        if err == io.EOF {
            return result, nil // EOF no es un error
        }
        if err != nil {
            return nil, fmt.Errorf("read: %w", err)
        }
    }
}
// En la práctica: io.ReadAll(r) hace exactamente esto
```

---

## io.Writer — El Contrato

```go
n, err := w.Write(p)
```

- Si `n < len(p)` y `err == nil` → bug en la implementación del Writer (viola el contrato)
- `io.ErrShortWrite` — el Writer escribió menos bytes de los pedidos sin reportar error
- Siempre verificar `err` después de `Write` en Writers de red o archivo

---

## bufio — Por Qué Existe

Cada llamada a `Read`/`Write` sobre un `os.File` o una conexión TCP es una **syscall**. Las syscalls son costosas — del orden de microsegundos. `bufio` acumula datos en memoria y hace pocas syscalls grandes en lugar de muchas syscalls pequeñas.

```
Sin bufio:   Write("h") → syscall → Write("e") → syscall → Write("l") → syscall...
Con bufio:   Write("h") → buffer | Write("e") → buffer | Flush() → 1 syscall con todo
```

### bufio.Reader

```go
// Lee líneas completas sin cargar el archivo entero en memoria
f, _ := os.Open("large.log")
defer f.Close()

br := bufio.NewReader(f)           // buffer por defecto: 4096 bytes
// o: bufio.NewReaderSize(f, 64*1024) para buffer más grande

for {
    line, err := br.ReadString('\n') // retorna incluyendo el delimitador
    if len(line) > 0 {
        process(line) // procesar aunque err != nil (puede ser EOF con datos)
    }
    if err == io.EOF {
        break
    }
    if err != nil {
        return fmt.Errorf("read line: %w", err)
    }
}
```

### bufio.Scanner — La Abstracción de Alto Nivel

```go
// Scanner es más ergonómico que ReadString para líneas
scanner := bufio.NewScanner(r)
scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // aumentar para líneas largas

for scanner.Scan() {
    line := scanner.Text() // string sin el '\n' final
    process(line)
}
if err := scanner.Err(); err != nil { // EOF ya está manejado internamente
    return fmt.Errorf("scan: %w", err)
}
```

### bufio.Writer — El Footgun del Flush

```go
f, _ := os.Create("output.txt")
defer f.Close()

bw := bufio.NewWriter(f)
defer bw.Flush() // CRÍTICO: sin esto, los datos en el buffer se pierden al cerrar

fmt.Fprintln(bw, "line 1")
fmt.Fprintln(bw, "line 2")
// Si el proceso termina aquí sin Flush() → "output.txt" puede estar vacío
```

**Orden correcto de defers:**
```go
f, _ := os.Create("output.txt")
defer f.Close()        // se ejecuta segundo

bw := bufio.NewWriter(f)
defer bw.Flush()       // se ejecuta primero (LIFO) — flushea antes de cerrar el archivo
```

---

## Composición de Readers y Writers

La potencia real del modelo está en encadenar implementaciones sin materializar datos intermedios en memoria.

### io.TeeReader — Leer y Copiar Simultáneamente

```go
// Lee de src, escribe CADA byte leído en log, retorna un Reader que da los mismos bytes
tee := io.TeeReader(src, log)

// Ahora cualquier Read en tee también escribe en log
hash := sha256.New()
tee2 := io.TeeReader(src, hash) // calcula hash mientras lees
io.Copy(dst, tee2)
fmt.Printf("SHA256: %x\n", hash.Sum(nil))
```

### io.MultiWriter — Un Write, Múltiples Destinos

```go
// Escribe a todos los writers en orden; falla si cualquiera falla
w := io.MultiWriter(file, os.Stdout, hashWriter)
json.NewEncoder(w).Encode(data) // va a los tres simultáneamente
```

### io.LimitReader — Protección Contra Inputs Maliciosos

```go
// SIEMPRE limitar el body de un HTTP request — sin límite es un vector de DoS
const maxBodySize = 1 << 20 // 1 MB
limited := io.LimitReader(r.Body, maxBodySize+1)
body, err := io.ReadAll(limited)
if len(body) > maxBodySize {
    return errors.New("request body too large")
}
```

### io.Pipe — Conectar un Writer con un Reader

`io.Pipe` crea un pipe síncrono en memoria: lo que se escribe en `PipeWriter` aparece en `PipeReader`. Los writes bloquean hasta que el reader consume los datos — sin buffer, sin allocaciones intermedias.

```go
// Caso de uso: comprimir datos mientras se envían por HTTP
pr, pw := io.Pipe()

go func() {
    defer pw.Close()
    gw := gzip.NewWriter(pw)
    defer gw.Close()
    json.NewEncoder(gw).Encode(largeStruct) // escribe → gzip → pipe
}()

// pr se puede pasar directamente como body de la request
req, _ := http.NewRequestWithContext(ctx, "POST", url, pr)
req.Header.Set("Content-Encoding", "gzip")
http.DefaultClient.Do(req)
```

### Pipeline Completo — Sin Materializar Nada en Memoria

```go
// Descarga un archivo → descomprime → hashea → sube a S3
// El archivo puede pesar GB; el proceso usa O(buffer) memoria

resp, err := http.Get(sourceURL)
if err != nil {
    return err
}
defer resp.Body.Close()

pr, pw := io.Pipe()
hash := sha256.New()

go func() {
    defer pw.Close()
    gz, _ := gzip.NewReader(resp.Body) // descomprime
    defer gz.Close()
    tee := io.TeeReader(gz, hash)      // hashea mientras pasa
    io.Copy(pw, tee)                   // escribe al pipe
}()

// Sube desde el pipe — nunca toca el disco
if err := s3.Upload(ctx, pr); err != nil {
    return fmt.Errorf("upload: %w", err)
}
fmt.Printf("checksum: %x\n", hash.Sum(nil))
```

---

## bytes.Buffer vs strings.Builder

| | `bytes.Buffer` | `strings.Builder` |
|---|---|---|
| Implementa | `io.Reader` + `io.Writer` | Solo `io.Writer` |
| Resultado final | `[]byte` o `string` | Solo `string` |
| Reset | `buf.Reset()` | `sb.Reset()` |
| Cuándo usar | Necesitas leer Y escribir en el mismo buffer | Solo construir un string |

```go
// bytes.Buffer: útil como puente entre Writers y Readers
var buf bytes.Buffer
json.NewEncoder(&buf).Encode(payload)  // escribe como Writer
http.Post(url, "application/json", &buf) // lee como Reader

// strings.Builder: concatenación eficiente
var sb strings.Builder
sb.Grow(estimatedSize) // evita re-allocaciones
for _, part := range parts {
    sb.WriteString(part)
    sb.WriteByte('\n')
}
result := sb.String() // O(1) — no copia, comparte el buffer interno
```

---

## os.File, net/http y el Modelo Unificado

La elegancia del modelo: `*os.File`, `http.Response.Body`, y `http.ResponseWriter` son todos Readers o Writers — no hay API especial para cada uno.

```go
// Servir un archivo sin cargarlo en memoria
func serveFile(w http.ResponseWriter, r *http.Request) {
    f, err := os.Open("large.bin")
    if err != nil {
        http.Error(w, "not found", 404)
        return
    }
    defer f.Close()

    w.Header().Set("Content-Type", "application/octet-stream")
    io.Copy(w, f) // os.File (Reader) → http.ResponseWriter (Writer)
}

// Streaming de respuesta HTTP a archivo
func downloadToFile(url, path string) error {
    resp, err := http.Get(url)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    f, err := os.Create(path)
    if err != nil {
        return err
    }
    defer f.Close()

    bw := bufio.NewWriterSize(f, 1<<20) // buffer de 1MB para I/O eficiente
    defer bw.Flush()

    _, err = io.Copy(bw, resp.Body) // http.Body (Reader) → bufio.Writer (Writer)
    return err
}
```

---

## Interfaces de I/O Más Usadas de la Stdlib

```go
io.Reader          // Read(p []byte) (n int, err error)
io.Writer          // Write(p []byte) (n int, err error)
io.Closer          // Close() error
io.ReadCloser      // Reader + Closer (http.Response.Body)
io.WriteCloser     // Writer + Closer (gzip.Writer)
io.ReadWriter      // Reader + Writer (net.Conn, bytes.Buffer)
io.ReadWriteCloser // Reader + Writer + Closer (os.File, net.Conn)
io.Seeker          // Seek(offset int64, whence int) (int64, error)
io.ReadSeeker      // Reader + Seeker (os.File, bytes.Reader)
io.WriterTo        // WriteTo(w Writer) (n int64, err error) — implementado por bytes.Buffer
io.ReaderFrom      // ReadFrom(r Reader) (n int64, err error) — implementado por os.File
```

`io.Copy` verifica si src implementa `WriterTo` o si dst implementa `ReaderFrom` — si es así, delega la copia a esa implementación que puede ser más eficiente (ej. `sendfile` syscall para `os.File`).

---

## Checklist de I/O

- [ ] `bufio.Reader/Writer` usado para cualquier I/O de archivo o red
- [ ] `defer bw.Flush()` declarado inmediatamente después de `bufio.NewWriter`
- [ ] `io.LimitReader` aplicado antes de leer de fuentes externas (HTTP, uploads)
- [ ] El caso `n > 0, err == io.EOF` manejado correctamente — procesar `n` antes de retornar
- [ ] `defer rows.Close()` / `defer resp.Body.Close()` — siempre cerrar Readers externos
- [ ] `io.Pipe` preferido sobre `bytes.Buffer` cuando el tamaño del dato es desconocido o grande
- [ ] `strings.Builder` con `Grow` para construir strings en loops
- [ ] `bytes.Buffer` solo cuando se necesita tanto `Read` como `Write` en el mismo buffer
