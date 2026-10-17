# Channels & Concurrency Fundamentals — Go Idiomatic Reference

## Concurrencia vs Paralelismo

Son ejes distintos. **Concurrencia** es estructura: descomponer un programa en tareas independientes que progresan de forma intercalada. **Paralelismo** es ejecución: correr varias de esas tareas literalmente al mismo tiempo sobre varios cores. Un programa concurrente corre bien en un solo core; el paralelismo solo aparece si hay hardware para ello.

Una goroutine es un hilo ligero gestionado por el runtime, no por el SO — arranca con ~2 KB de stack que crece bajo demanda. El scheduler es **G-P-M**: `G` goroutines se multiplexan sobre `M` threads del SO a través de `P` procesadores lógicos, cada `P` con su run queue local y work-stealing entre ellas.

`GOMAXPROCS` fija cuántos `P` hay activos, es decir el techo de paralelismo real. Por defecto es el número de cores visibles (o el límite de CPU del cgroup en Go 1.25+). Consúltalo sin modificarlo con `runtime.GOMAXPROCS(0)`.

Regla: elige la concurrencia por la estructura del problema (cuántas tareas independientes hay), no por cuántos cores tienes. El scheduler reparte.

---

## Axiomas de Channels

Un channel tiene tres estados — nil, abierto, cerrado — y cada operación se comporta distinto en cada uno. Memorizar esta tabla elimina la mayoría de los deadlocks y panics de concurrencia.

| Estado del canal | `send` (`ch <- v`) | `recv` (`<-ch`) | `close(ch)` |
|---|---|---|---|
| `nil` | bloquea **para siempre** | bloquea **para siempre** | **panic** |
| abierto | bloquea si no hay hueco/receptor | bloquea si no hay dato | ok |
| cerrado | **panic** | zero value + `ok=false` (inmediato) | **panic** (doble close) |

Dos consecuencias que se explotan a diario:

- El bloqueo eterno de un canal `nil` es **útil** dentro de un `select`: asignar `nil` a una variable de canal desactiva ese `case` dinámicamente (ver más abajo).
- Un canal cerrado se puede leer infinitas veces; primero drena lo que quede en el buffer y luego entrega zero values con `ok=false`. Por eso `range ch` termina limpio al cerrar.

```go
ch := make(chan int, 1)
ch <- 42
close(ch)
v, ok := <-ch // v=42, ok=true  — drena el buffer primero
v, ok = <-ch  // v=0,  ok=false — cerrado y vacío
```

### Ownership: quién cierra

**Cierra el productor/owner del canal, NUNCA el receptor.** El `close` es una señal de "no habrá más datos", y solo quien produce sabe eso. Cerrar desde el receptor abre la puerta a un `send` sobre canal cerrado (panic) en el productor.

```go
func producer(n int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out) // el owner cierra al terminar de producir
		for i := 0; i < n; i++ {
			out <- i
		}
	}()
	return out // devuelve <-chan int: el receptor no puede cerrarlo
}

func consume() {
	for v := range producer(3) { // range termina solo cuando el owner cierra
		fmt.Println(v)
	}
}
```

Devolver `<-chan T` (receive-only) hace que el compilador impida que el receptor cierre el canal. Es el modo idiomático de codificar el ownership en la firma.

---

## Unbuffered vs Buffered

Un canal **unbuffered** (`make(chan T)`, capacidad 0) es un **rendezvous síncrono**: el `send` y el `recv` se encuentran en el mismo instante y hay un handoff garantizado — el emisor no avanza hasta que alguien recibe. Un canal **buffered** (`make(chan T, N)`) es una **cola asíncrona** de capacidad `N`: el `send` solo bloquea si el buffer está lleno; el `recv` solo bloquea si está vacío.

| | Unbuffered (`cap==0`) | Buffered (`cap==N`) |
|---|---|---|
| Semántica | rendezvous síncrono | cola asíncrona FIFO |
| `send` bloquea | hasta que un `recv` lo tome | solo si el buffer está lleno |
| `recv` bloquea | hasta que un `send` ocurra | solo si el buffer está vacío |
| Garantía tras `send` | el receptor **ya** lo tomó | solo encolado, quizá no leído |
| Uso típico | señalización, handoff, sincronización | desacoplar productor/consumidor |

`len()` y `cap()` operan sobre channels: `cap(ch)` es el tamaño del buffer (constante); `len(ch)` es cuántos elementos están encolados ahora mismo. Sobre un unbuffered ambos son 0.

```go
ch := make(chan int, 2)
ch <- 1
ch <- 2                       // no bloquea: hay hueco
fmt.Println(len(ch), cap(ch)) // 2 2
<-ch
fmt.Println(len(ch), cap(ch)) // 1 2
```

`len(ch)` es una foto instantánea sin valor para sincronizar — puede cambiar entre que lo lees y actúas. Úsalo para métricas, no para control de flujo.

---

## Cuándo Buffered es Correcto — y el Anti-Patrón

Un buffer se justifica cuando hay una razón concreta:

- **`N` conocido de antemano.** Un resultado por worker con `N` fijo: los sends nunca bloquean y el receptor puede recogerlos a su ritmo.
- **Desacoplar productor de consumidor** cuando su throughput es desigual y ráfagas cortas no deben frenar al productor.
- **Evitar goroutine leak cuando el receptor puede irse.** Si el receptor abandona (timeout, cancelación) antes de leer, un `send` sobre canal unbuffered bloquea a la goroutine emisora para siempre. Un buffer de tamaño suficiente absorbe el envío y deja que la goroutine termine.

```go
func fanOut(ids []int) <-chan int {
	results := make(chan int, len(ids)) // N exacto: los sends nunca bloquean
	for _, id := range ids {
		go func(id int) {
			results <- id * id // sin leak aunque nadie lea aún
		}(id)
	}
	return results
}
```

### Anti-patrón: usar un buffer para "arreglar" un bug

```go
// BAD: aumentar el buffer para que el deadlock/race "desaparezca"
ch := make(chan int, 1000) // ← número mágico que oculta el problema

// GOOD: diseñar la terminación y la sincronización explícitamente
ch := make(chan int)       // capacidad = razón concreta, no conveniencia
```

Un buffer **no corrige** un deadlock ni una race — solo cambia el timing lo suficiente para que el síntoma se manifieste menos seguido. Si eliges el tamaño del buffer por prueba y error hasta que "deja de fallar", el bug sigue ahí, latente y dependiente de la carga. La capacidad de un canal debe derivarse de una propiedad del problema (número de workers, tamaño de lote), nunca ajustarse para esconder un fallo de sincronización.

---

## select

`select` espera sobre varias operaciones de canal a la vez y procede con la primera que esté lista.

- Si **varios** casos están listos, elige uno **al azar** (evita starvation; no asumas orden).
- Un caso `default` hace el `select` **no bloqueante**: si ningún canal está listo, corre `default` de inmediato.
- Asignar `nil` a una variable de canal **desactiva** ese caso: como leer/escribir en `nil` bloquea para siempre, ese `case` nunca vuelve a seleccionarse.

```go
// default no bloqueante: intenta enviar sin esperar
func trySend(ch chan int, v int) bool {
	select {
	case ch <- v:
		return true
	default:
		return false // canal lleno: no bloquea
	}
}
```

```go
// Truco del canal nil: desactivar un caso dinámicamente
func drain(in <-chan int, tick <-chan time.Time) {
	for in != nil || tick != nil {
		select {
		case v, ok := <-in:
			if !ok {
				in = nil // canal agotado → desactiva este case
				continue
			}
			fmt.Println(v)
		case <-tick:
			tick = nil // desactiva el ticker
		}
	}
}
```

El patrón `for`-`select` con `ctx.Done()` es la forma canónica de una goroutine terminable: siempre hay una salida garantizada.

```go
func worker(ctx context.Context, in <-chan int, out chan<- int) {
	for {
		select {
		case <-ctx.Done():
			return // única salida garantizada
		case v, ok := <-in:
			if !ok {
				return
			}
			select { // el send también respeta la cancelación
			case out <- v * 2:
			case <-ctx.Done():
				return
			}
		}
	}
}
```

---

## Memory Model / Happens-Before

Un channel no solo **comunica** valores: **sincroniza** memoria. El Go Memory Model garantiza dos relaciones happens-before clave:

- Un `send` sobre un canal **happens-before** el `recv` correspondiente completa.
- Un `close` **happens-before** un `recv` que observa el cierre (el que recibe `ok=false`).

Por eso, todo lo que una goroutine escribió **antes** de enviar (o cerrar) es visible en la goroutine que recibe. Sin esta garantía, una escritura en un core podría no ser visible en otro.

```go
func happensBefore() {
	var data int
	done := make(chan struct{})
	go func() {
		data = 42   // (1) escritura
		close(done) // (2) close happens-before el recv que lo observa
	}()
	<-done            // (3) observa el close
	fmt.Println(data) // 42 garantizado: (1) es visible aquí
}
```

Para el caso concreto de un **contador**, un channel es demasiado pesado. `sync/atomic` da la misma garantía de visibilidad con read-modify-write atómico y sin bloqueo:

```go
var n atomic.Int64 // Go 1.19+ tipos atómicos con métodos
n.Add(1)           // atómico: sin race, sin mutex
n.Load()
```

Regla: channels para transferir datos y coordinar; `atomic` para contadores y flags simples; `mutex` para invariantes sobre varias variables.

---

## Data Races

Una **data race** ocurre cuando dos goroutines acceden a la misma variable concurrentemente, al menos uno de los accesos es una escritura, y no hay una relación happens-before que los ordene. El resultado es undefined behavior: valores corruptos, no solo "el último gana".

El race detector es la herramienta definitiva. Instrumenta los accesos a memoria en runtime y reporta la race con ambos stacks:

```bash
go run -race ./...
go test -race ./...
```

```go
// BAD: counter++ es read-modify-write NO atómico → data race
func racyCounter() int {
	var counter int
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter++ // -race lo marca; el resultado es no determinista
		}()
	}
	wg.Wait()
	return counter
}
```

```go
// GOOD (mutex): protege el estado compartido
func mutexCounter() int {
	var (
		mu      sync.Mutex
		counter int
		wg      sync.WaitGroup
	)
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			counter++
			mu.Unlock()
		}()
	}
	wg.Wait()
	return counter // siempre 1000
}
```

```go
// GOOD (channel): un solo owner toca el estado; el resto le envía trabajo
func channelCounter() int {
	inc := make(chan struct{})
	result := make(chan int)
	go func() {
		counter := 0
		for range inc {
			counter++ // ninguna otra goroutine ve counter
		}
		result <- counter
	}()
	// ... goroutines hacen `inc <- struct{}{}`, luego close(inc)
	return <-result
}
```

Corre `-race` en CI sobre los tests concurrentes. No detecta races en caminos que el test no ejercita, pero cero falsos positivos: lo que reporta es real.

---

## "Don't communicate by sharing memory; share memory by communicating"

El proverbio de Go: en lugar de que varias goroutines lean y escriban la misma variable bajo locks, pasa la propiedad del dato por un channel — en cada instante solo una goroutine lo posee, y transferirlo es un `send`. Elimina la clase entera de bugs de "olvidé tomar el lock".

Pero tiene un **límite**, y tratarlo como dogma produce código peor:

| Usa **channels** cuando... | Usa **`sync.Mutex`** cuando... |
|---|---|
| transfieres la propiedad de un dato | proteges estado compartido de vida larga |
| coordinas el ciclo de vida de goroutines | el estado es un struct con invariantes entre campos |
| construyes un pipeline o fan-out/in | el acceso es un simple get/set con contención |
| la unidad natural es "un mensaje" | mover el dato por un canal sería más caro que un lock |

Un cache concurrente, un contador de conexiones, un registro de métricas — todo eso es estado compartido de vida larga que un `sync.Mutex` (o `sync.RWMutex`, o `atomic`) protege de forma más simple y rápida que orquestar una goroutine-dueña con channels. Forzar channels ahí añade una goroutine, latencia y complejidad sin ganancia. Elige la herramienta por la forma del problema, no por el eslogan.

---

## Checklist

- [ ] La capacidad del canal deriva de una razón concreta (N workers, tamaño de lote), nunca de prueba y error para tapar un bug.
- [ ] El productor/owner cierra el canal; el receptor nunca — codificado con `<-chan T` en la firma.
- [ ] Ningún `send` sobre un canal potencialmente cerrado, ni doble `close`, ni `close` de canal `nil`.
- [ ] Toda goroutine tiene salida garantizada: `for`-`select` con `case <-ctx.Done()`.
- [ ] Se usa un unbuffered para señalización/handoff y buffered solo para desacoplar o evitar leaks del emisor.
- [ ] `len(ch)`/`cap(ch)` solo para métricas, jamás para control de flujo de sincronización.
- [ ] Contadores y flags simples usan `sync/atomic`; estado con invariantes usa `sync.Mutex`; transferencia de propiedad usa channels.
- [ ] `go test -race` corre en CI sobre todo el código concurrente y pasa limpio.
