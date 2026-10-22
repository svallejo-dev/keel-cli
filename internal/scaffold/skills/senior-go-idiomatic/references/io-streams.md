# I/O, Streams, Buffers & Text — Go Idiomatic Reference

## Contents

- The mental model: `io.Reader` / `io.Writer` as contracts
- The full `io.Reader` contract (the four cases) and the `io.Writer` contract
- bufio: when buffering pays, `Scanner`, `Writer` and its `Flush` error
- Composing readers and writers: `TeeReader`, `MultiWriter`, size limits, `io.Pipe`
- `io.Copy` and the unified model (files, HTTP, sockets)
- Text: strings, runes, UTF-8, `strings.Builder` vs `bytes.Buffer`
- Checklist

---

## The Mental Model

`io.Reader` and `io.Writer` are contracts, not implementations. Anything that satisfies one
method composes with anything that satisfies the other: `*os.File`, `http.Response.Body`,
`bytes.Buffer`, `gzip.Reader`, `tls.Conn` all speak the same language.

```
io.Reader: Read(p []byte) (n int, err error)
io.Writer: Write(p []byte) (n int, err error)
```

One method each, and the standard library builds arbitrarily complex data pipelines from them.

---

## io.Reader — The Full Contract

`Read` can return in four states, and ignoring any of them is a bug:

| `n` | `err` | Meaning |
|---|---|---|
| `> 0` | `nil` | Data read; more may follow |
| `> 0` | `io.EOF` | Data read and the stream ended — **process the `n` bytes first** |
| `0` | `io.EOF` | Stream ended, no new data |
| `0` | other | Failure, no data |

`n > 0` together with an error is the common trap: code that discards `n` when it sees `io.EOF`
loses the last chunk.

```go
// Process n before looking at err.
func readAll(r io.Reader) ([]byte, error) {
	var out []byte
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read: %w", err)
		}
	}
}
```

In practice `io.ReadAll` does exactly this; the loop matters when you write your own reader or
consume one chunk at a time.

## io.Writer — The Contract

- `Write` must return a non-nil error when `n < len(p)`. A writer that writes less and returns
  `nil` violates the contract; helpers such as `io.Copy` report that as `io.ErrShortWrite`.
- `Write` must not modify or retain `p`.
- Check the error of every write to a file or network writer — or use a type that remembers it
  (`bufio.Writer`, see below) and check once at the end.

---

## bufio — When Buffering Pays

Each `Read`/`Write` on an `*os.File` or a network connection is a system call, which costs on the
order of microseconds. `bufio` batches many small operations into few large ones.

```
Without bufio:  Write("h") → syscall, Write("e") → syscall, Write("l") → syscall, ...
With bufio:     Write("h") → buffer,  Write("e") → buffer,  Flush()   → one syscall
```

Buffer when you do many small reads or writes (line-by-line parsing, `fmt.Fprintf` in a loop,
encoding records one by one). Do not wrap the destination of a single `io.Copy`: it already moves
32 KB chunks, so the extra layer only adds a `Flush` error to handle (see "io.Copy" below).

### bufio.Scanner — lines and tokens

```go
sc := bufio.NewScanner(r)
sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // allow lines up to 1 MB (default max is 64 KB)
for sc.Scan() {
	process(sc.Text()) // the line without the trailing newline
}
if err := sc.Err(); err != nil { // nil at EOF; non-nil for real errors and too-long lines
	return fmt.Errorf("scan: %w", err)
}
```

`Scanner` is the right tool for lines and words. A line longer than the buffer stops the scan
with `bufio.ErrTooLong`; when lines are unbounded, use `bufio.Reader.ReadString` or `ReadLine`.

### bufio.Writer — the Flush error is the write error

`bufio.Writer` keeps data in memory until the buffer fills or you call `Flush`. Two consequences:
data not flushed is lost, and an I/O error from an earlier write can surface only in `Flush`.
Once a write fails, every later `Write` and `Flush` returns the same error, so checking `Flush`
once at the end is enough — but it has to be checked, which a bare `defer bw.Flush()` never does.

**export.go**
```go
func writeLines(path string, lines []string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		// Close can report a deferred write error; keep the first error.
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	bw := bufio.NewWriter(f)
	for _, l := range lines {
		fmt.Fprintln(bw, l) // errors are sticky in bw; Flush reports them
	}
	return bw.Flush()
}
```

The order matters: `Flush` runs before the deferred `Close`, and both errors reach the caller
through the named result. The same shape applies to `gzip.Writer.Close`, `csv.Writer.Flush` +
`Error`, and anything else that buffers.

---

## Composing Readers and Writers

The power of the model is chaining implementations without materializing intermediate data.

### io.TeeReader — read and observe at once

```go
// Every byte read from body is also written into h.
h := sha256.New()
n, err := io.Copy(dst, io.TeeReader(body, h))
if err != nil {
	return fmt.Errorf("copy: %w", err)
}
fmt.Printf("%d bytes, sha256 %x\n", n, h.Sum(nil))
```

### io.MultiWriter — one write, several destinations

```go
// Writes go to each writer in order; the first error stops the write.
w := io.MultiWriter(f, h)
if err := json.NewEncoder(w).Encode(record); err != nil {
	return fmt.Errorf("encode: %w", err)
}
```

### Size limits on untrusted input

Reading an unbounded body from the network is a memory-exhaustion vector.

For **incoming HTTP request bodies**, use `http.MaxBytesReader`: it caps the body, returns a
`*http.MaxBytesError` past the limit, and tells the server to close the connection instead of
reading the rest.

```go
const maxBody = 1 << 20 // 1 MB

func create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var in createInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	// ...
}
```

For **other readers** (an HTTP client response, an upload you stream to disk), wrap with
`io.LimitReader(r, max+1)` and treat reading more than `max` bytes as an error; `LimitReader` by
itself just stops at the limit and reports EOF.

### io.Pipe — connect a writer to a reader

`io.Pipe` is a synchronous in-memory pipe: what is written to the `PipeWriter` comes out of the
`PipeReader`. Writes block until the reader consumes them, so there is no buffer and no
intermediate copy. The writer side must end with `CloseWithError(err)`: with `nil` the reader
sees `io.EOF`; with an error the reader sees that error instead of a truncated stream that looks
complete.

```go
// Compress and send without building the whole payload in memory.
func postGzipJSON(ctx context.Context, c *http.Client, url string, v any) error {
	pr, pw := io.Pipe()
	go func() {
		gw := gzip.NewWriter(pw)
		err := json.NewEncoder(gw).Encode(v)
		if cerr := gw.Close(); err == nil {
			err = cerr // Close writes the gzip footer; it can fail too
		}
		pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
	if err != nil {
		pr.Close()
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("post: status %s", resp.Status)
	}
	return nil
}
```

If the request fails, the transport closes the body (`pr`), so the goroutine's next write returns
an error and it exits instead of leaking.

### A streaming pipeline with no full copy in memory

```go
// Download → decompress → hash → upload. Memory stays O(buffer) for multi-GB files.
func mirror(ctx context.Context, src io.Reader, upload func(context.Context, io.Reader) error) (sum []byte, err error) {
	pr, pw := io.Pipe()
	h := sha256.New()
	go func() {
		gz, err := gzip.NewReader(src)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("gunzip: %w", err))
			return
		}
		_, err = io.Copy(pw, io.TeeReader(gz, h))
		if cerr := gz.Close(); err == nil {
			err = cerr
		}
		pw.CloseWithError(err)
	}()

	if err := upload(ctx, pr); err != nil {
		pr.CloseWithError(err) // unblocks the producer
		return nil, fmt.Errorf("upload: %w", err)
	}
	return h.Sum(nil), nil
}
```

The checksum is only meaningful once `upload` has read the pipe to EOF, which is what a
successful upload means.

---

## io.Copy and the Unified Model

`*os.File`, `http.Response.Body` and `http.ResponseWriter` are all readers or writers; there is no
special API for each.

`io.Copy(dst, src)` uses `src.WriteTo` if `src` implements `io.WriterTo`, otherwise
`dst.ReadFrom` if `dst` implements `io.ReaderFrom`, otherwise a 32 KB buffer. `*os.File`,
`*net.TCPConn` and the HTTP server's response writer implement these so that, on Linux,
file-to-socket copies can use `sendfile`/`splice` and skip user space entirely. Wrapping either
side in your own type that hides those methods silently falls back to the generic loop.

```go
// Serve a file without loading it into memory. http.ServeContent also handles
// Range requests, If-Modified-Since and Content-Type sniffing.
func serveFile(w http.ResponseWriter, r *http.Request) {
	f, err := os.Open("large.bin")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		http.Error(w, "stat failed", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// Stream an HTTP response to a file.
func downloadToFile(ctx context.Context, url, path string) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("get %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get %s: status %s", url, resp.Status)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	// No bufio: io.Copy already writes in large chunks and can use f.ReadFrom.
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	return nil
}
```

The most used I/O interfaces:

```go
io.Reader          // Read(p []byte) (n int, err error)
io.Writer          // Write(p []byte) (n int, err error)
io.Closer          // Close() error
io.ReadCloser      // Reader + Closer (http.Response.Body)
io.WriteCloser     // Writer + Closer (gzip.Writer)
io.ReadWriter      // Reader + Writer (bytes.Buffer)
io.ReadWriteCloser // Reader + Writer + Closer (os.File, net.Conn)
io.Seeker          // Seek(offset int64, whence int) (int64, error)
io.ReadSeeker      // Reader + Seeker (os.File, bytes.Reader, strings.Reader)
io.WriterTo        // WriteTo(w Writer) (n int64, err error) — bytes.Buffer, os.File
io.ReaderFrom      // ReadFrom(r Reader) (n int64, err error) — os.File, bufio.Writer
```

---

## Text: Strings, Runes and UTF-8

A Go `string` is an immutable sequence of bytes, usually UTF-8, not a sequence of characters. A
`rune` is an `int32` holding one Unicode code point. The difference shows up as soon as the text
leaves ASCII.

```go
s := "café"
fmt.Println(len(s))                    // 5 — bytes ("é" is 2 bytes in UTF-8)
fmt.Println(utf8.RuneCountInString(s)) // 4 — code points

for i := 0; i < len(s); i++ {
	fmt.Printf("%c", s[i]) // "cafÃ©": each byte printed as if it were a code point
}
for i, r := range s { // range over a string decodes runes; i is the byte offset
	fmt.Println(i, string(r)) // 0 c, 1 a, 2 f, 3 é (the next offset would be 5)
}
```

Index a string (`s[i]`, `s[a:b]`) only with byte offsets you got from a search
(`strings.Index`) or from `range`; slicing at an arbitrary byte can split a rune. A code point is
not always a user-perceived character either (combining accents, emoji sequences); when that
matters, use a grapheme library or `golang.org/x/text`.

Useful functions and their less obvious behavior:

```go
strings.Cut(s, sep)          // before, after, found — replaces most Index+slice code
strings.Fields(s)            // split on any run of Unicode whitespace
strings.EqualFold(a, b)      // case-insensitive comparison without allocating
strings.ToLower(s)           // Unicode-aware; avoid for comparisons (use EqualFold)
strings.TrimSpace(s)         // trims Unicode whitespace at both ends
utf8.ValidString(s)          // validate untrusted input before treating it as text
```

### Building strings

```go
// Each += copies the whole string so far: quadratic in the number of parts.
var out string
for _, p := range parts {
	out += p
}

// strings.Builder appends into one growing buffer; String() does not copy it.
var b strings.Builder
b.Grow(estimate) // optional: one allocation when you know the size
for _, p := range parts {
	b.WriteString(p)
}
out = b.String()
```

For joining a slice with a separator, `strings.Join` is simpler still.

### strings.Builder vs bytes.Buffer

| | `strings.Builder` | `bytes.Buffer` |
|---|---|---|
| Implements | `io.Writer` (and `WriteString`, `WriteByte`, `WriteRune`) | `io.Reader` and `io.Writer` |
| Result | `string`, without copying | `[]byte` (`Bytes`) or `string` (copies) |
| Use when | building a final string | you write and then read the same buffer |

```go
// bytes.Buffer as a bridge: encode as a Writer, send as a Reader.
var buf bytes.Buffer
if err := json.NewEncoder(&buf).Encode(payload); err != nil {
	return fmt.Errorf("encode: %w", err)
}
resp, err := http.Post(url, "application/json", &buf)
```

Do not copy a non-zero `strings.Builder` or `bytes.Buffer` by value; pass a pointer.

### []byte ↔ string conversions

`[]byte(s)` and `string(b)` normally allocate and copy, because strings are immutable and byte
slices are not. In hot paths, keep data in one form. The compiler skips the copy in a few
recognized patterns, such as a map lookup `m[string(b)]` and a comparison `string(b) == "GET"`.
Check other cases with `go build -gcflags=-m` or a benchmark with `-benchmem` rather than
assuming.

---

## Checklist

- [ ] The `n > 0` bytes returned together with an error (including `io.EOF`) are processed
- [ ] `bufio` used where there are many small reads/writes, not in front of a single `io.Copy`
- [ ] `bufio.Writer`: `Flush` called and its error returned; `Close` errors kept with a named result
- [ ] Incoming request bodies capped with `http.MaxBytesReader`; other untrusted readers with
      `io.LimitReader` plus an explicit "too large" check
- [ ] `io.Pipe` writers end with `CloseWithError(err)`; readers that give up close their side
- [ ] `resp.Body.Close()` deferred after a successful request; status code checked
- [ ] Strings iterated with `range` (runes) when the text may be non-ASCII; `len` means bytes
- [ ] `strings.Builder` (or `strings.Join`) instead of `+=` in loops; `EqualFold` for
      case-insensitive comparisons
- [ ] `bytes.Buffer` only when the same buffer is written and then read
