# Entities — DDD Táctico al Estilo Idiomático de Go

> **Regla #0 — go-idiomatic gana.** Ante cualquier conflicto entre un patrón de DDD y el Go idiomático, gana el Go idiomático. Una entity en Go **no** es un JavaBean: sin getters `Get*`, sin setters, sin modelo anémico. Modelamos identidad y estado mutable con las herramientas del lenguaje (structs, pointer receivers, tipos dedicados), no con ceremonia importada de otra plataforma.

Verificado con toolchain 1.24.7; API estándar de Go 1.26. Los VOs (`Email`, `Name`) que se componen aquí son stubs mínimos; su mecánica interna vive en `references/value-objects.md`.

Este documento cubre **solo** la entity. Las fronteras adyacentes son deliberadas:

- La construcción, normalización e igualdad por valor de un VO → `references/value-objects.md` (aquí solo se **componen**).
- La raíz que agrupa entities y VOs bajo un límite transaccional → `references/aggregates.md`.
- La orquestación de casos de uso, transacciones y persistencia → `references/application-layer.md`.
- El modelo anémico y su refactor → `references/antipatterns.md`.

---

## 1. Qué ES una Entity

Una **entity** es un objeto del dominio definido por su **identidad**, no por sus atributos:

- Tiene un **ID** estable que la identifica de forma única y persistente.
- Es **mutable a lo largo del tiempo**: sus atributos cambian, su identidad no.
- Está **compuesta por value objects** — cada atributo con reglas propias es un VO, no un tipo primitivo desnudo.

El contraste con el value object es total: un VO se define por su valor y es inmutable; una entity se define por su ID y evoluciona. Un `User` sigue siendo el mismo `User` aunque cambie de email, de nombre y de todo lo demás — porque su `id` no cambió.

---

## 2. Realización en Go

Una entity es un `struct` con **campos no-exportados** (encapsulación real: nadie fuera del paquete construye un estado inválido) y **métodos con pointer receiver** (mutan el estado y transmiten identidad).

```go
type UserID int64

// User es una persona registrada; su identidad es id.
type User struct {
	id        UserID
	email     Email
	name      Name
	createdAt time.Time
}
```

El ID es un **tipo propio** (`type UserID int64`), no un `int64` suelto ni un `string`. También puede ser un VO de UUID. La ganancia es del type system: pasar un `OrderID` donde se espera un `UserID` no compila.

### Pointer receiver (entity) vs value receiver (VO)

El tipo de receiver no es una preferencia estética: codifica la semántica del dominio.

| Aspecto | Entity | Value object |
|---|---|---|
| Receiver | `func (u *User)` — pointer | `func (e Email)` — value |
| Muta estado | Sí (`u.email = ...`) | Nunca; devuelve un nuevo valor |
| Identidad | Sí, por ID | No; es puro valor |
| Igualdad | Por ID (`SameAs`) | Por valor (`==`) |
| Copiar | Se comparte por puntero | Se copia libremente |

```go
// VO: value receiver, no muta, igualdad por valor.
func (e Email) String() string { return e.addr }

// Entity: pointer receiver, muta, identidad por ID.
func (u *User) ChangeEmail(newEmail Email) error { /* ... */ }
```

Regla práctica: si un método necesita cambiar el estado del receiver, o si el tipo tiene identidad, **todos** sus métodos usan pointer receiver (consistencia de method set). No mezcles value y pointer receivers en el mismo tipo.

### Zero value y por qué el constructor es obligatorio

El zero value de una entity es **inválido**: `id` en cero, VOs vacíos, sin `createdAt`. Los campos no-exportados son precisamente lo que impide construir ese estado inválido desde otro paquete: la única puerta de entrada es `NewUser`, que garantiza el invariante. Exportar los campos abre esa puerta y la entity deja de proteger nada.

```go
// BAD — campos exportados: cualquiera fabrica un User inválido sin pasar por NewUser.
type User struct {
	ID    UserID
	Email Email
}
u := User{} // id 0, email vacío: un estado que el dominio nunca debería ver

// GOOD — campos no-exportados + constructor.
u, err := NewUser(id, email, name)
```

---

## 3. Igualdad por IDENTIDAD, no por Valor

Dos entities son la **misma** si comparten ID, aunque cada atributo difiera. Y son **distintas** si tienen IDs distintos, aunque todos los demás atributos coincidan. Esto es lo opuesto a un VO.

Por eso **nunca** uses `==` sobre entities. `==` compara valor campo a campo: dos `User` con el mismo ID pero distinto `createdAt` saldrían "distintos", y peor aún, incluir un campo no-comparable (un slice, un map) haría que `==` ni siquiera compile. La identidad se compara explícitamente:

```go
// SameAs compara identidad, no valor: misma entity si comparten ID.
func (u *User) SameAs(other *User) bool { return u.id == other.id }
```

```go
// BAD — compara valor; frágil y semánticamente incorrecto para una entity.
if a == b { /* ... */ }

// GOOD — compara identidad, que es lo único estable de una entity.
if a.SameAs(b) { /* ... */ }
```

El `==` sobre el **ID** (`u.id == other.id`) sí es correcto: el `UserID` es un value object numérico y su igualdad por valor es exactamente lo que queremos.

### Identidad en colecciones

La misma regla gobierna cómo se indexan o deduplican entities: la clave es siempre el **ID**, nunca el valor entero del struct. Un `map[UserID]*User` colapsa duplicados por identidad de forma natural.

```go
// Indexar/deduplicar por identidad: la clave es el ID.
byID := make(map[UserID]*User, len(users))
for _, u := range users {
	byID[u.ID()] = u // dos cargas de la misma entity colapsan en una
}
```

Usar la entity misma como clave de un map (`map[User]...`) es un error de diseño: obliga a que todos sus campos sean comparables y ata la clave al valor mutable, no a la identidad.

---

## 4. El Constructor Valida la INTEGRACIÓN de los VOs

Un constructor de entity **no re-valida los VOs**: cuando `Email` y `Name` llegan al constructor, ya son válidos por construcción (sus propios constructores fallaron si no lo eran; ver `references/value-objects.md`). La entity valida las reglas que **cruzan** varios VOs — invariantes que ningún VO puede conocer por sí solo.

```go
// NewUser exige nombre y apellido cuando el email es corporativo.
func NewUser(id UserID, email Email, name Name) (*User, error) {
	if email.IsCorporate() && len(strings.Fields(name.String())) < 2 {
		return nil, fmt.Errorf("user: email corporativo requiere nombre y apellido")
	}
	return &User{
		id:        id,
		email:     email,
		name:      name,
		createdAt: time.Now(),
	}, nil
}
```

La firma recibe **VOs, no strings**: `NewUser(id UserID, email Email, name Name)`, jamás `NewUser(id int64, email, name string)`. Empujar la validación de cada campo a su VO deja al constructor un único trabajo claro: verificar la combinación. Si una entity crece con demasiadas invariantes cruzadas o coordina varios sub-objetos con reglas propias, probablemente es un **aggregate root** (ver `references/aggregates.md`).

El reparto de responsabilidades es nítido y no se solapa:

| Regla | Dónde vive | Ejemplo |
|---|---|---|
| Formato/rango de un solo campo | Constructor del VO | `NewEmail` exige un `@` |
| Normalización de un solo campo | Constructor del VO | `NewEmail` baja a minúsculas |
| Combinación entre varios VOs | Constructor de la entity | email corporativo ⇒ nombre y apellido |
| Transición de estado válida | Método de la entity | `ChangeEmail` re-verifica el cruce |
| Coordinación entre entities/aggregates | Capa de aplicación | ver `references/application-layer.md` |

Devolver `(*User, error)` (puntero + error), no `(User, error)`: la entity tiene identidad y se comparte por referencia; el error obliga a quien construye a manejar el caso inválido antes de tener un `User`.

---

## 5. Comportamiento con INTENCIÓN, no Setters

El estado mutable de una entity se cambia mediante métodos que **expresan la intención del dominio** y **re-verifican las invariantes**, no mediante setters mecánicos que dejan pasar cualquier valor.

```go
// ChangeEmail re-verifica la regla de email corporativo antes de cambiarlo.
func (u *User) ChangeEmail(newEmail Email) error {
	if newEmail.IsCorporate() && len(strings.Fields(u.name.String())) < 2 {
		return fmt.Errorf("user: email corporativo requiere nombre y apellido")
	}
	u.email = newEmail
	return nil
}
```

```go
// BAD — setter anémico: no expresa intención y puede romper la invariante.
func (u *User) SetEmail(e Email) { u.email = e }

// GOOD — método con nombre de dominio que protege el invariante.
func (u *User) ChangeEmail(newEmail Email) error { /* ... */ }
```

Sobre los accessors: en Go **no** existe el prefijo `Get`. `GetEmail()` es un Java-ismo; se escribe `Email()`. Y ni siquiera se expone por reflejo: un accessor se agrega **solo cuando de verdad hace falta** que otro paquete lea ese atributo. Muchas entities exponen comportamiento y casi ningún campo.

```go
func (u *User) Email() Email { return u.email }
func (u *User) ID() UserID   { return u.id }
```

Nótese que el accessor devuelve el **VO** (`Email`), no su representación interna (`string`). Devolver el VO preserva las garantías; devolver el string las tira.

El nombre del método **es** ubiquitous language: `Deactivate`, `Promote`, `Rename`, `ChangeEmail` describen operaciones que el negocio reconoce. Un `Update(fields map[string]any)` genérico borra esa intención y reabre la puerta a estados inválidos; cada transición merece su método con nombre propio y su verificación de invariantes.

---

## 6. Rich vs Anémico

La lógica vive **con** los datos, en el mismo paquete que el tipo. Una entity rica es un tipo cuyos métodos son el comportamiento del dominio: `u.ChangeEmail(...)`, `u.Deactivate()`, `u.Promote(...)`.

El **modelo anémico** es el anti-patrón inverso: la entity queda reducida a un saco de campos públicos (un DTO) y toda la lógica se exilia a un `UserService` gordo que la manipula desde fuera. Nadie es dueño de las invariantes; tres paquetes deben coordinarse para hacer cualquier cosa con un usuario.

```go
// BAD — anémico: datos sin comportamiento + lógica en un "service" externo.
package models
type User struct { // campos públicos, sin invariantes
	ID    int64
	Email string
	Name  string
}

package services
type UserService struct{}
func (UserService) ChangeEmail(u *models.User, e string) { u.Email = e } // sin protección
```

```go
// GOOD — rico: el tipo y su comportamiento en el mismo paquete de dominio.
package user
func (u *User) ChangeEmail(newEmail Email) error { /* protege invariantes */ }
```

Esta separación `models/` + `services/` es capa técnica disfrazada de arquitectura, no diseño de dominio. El desarrollo completo del anti-patrón y su refactor están en `references/antipatterns.md`. La orquestación legítima (transacciones, coordinación entre agregados) sí vive fuera de la entity, pero no en un `UserService`: cada caso de uso es su propio tipo en su propio archivo (`change_email.go` → `ChangeEmail`, que carga el `User`, llama a `u.ChangeEmail(...)` y lo guarda). Ver `references/application-layer.md`.

---

## 7. La Entity NO Toca Infraestructura

Una entity opera **solo** sobre VOs y tipos de dominio. Su lista de imports es el diagnóstico más rápido de si el diseño está sano — el **test de pureza del import**:

```go
// GOOD — imports de una entity sana: stdlib de dominio puro.
import (
	"fmt"
	"strings"
	"time"
)
```

```go
// BAD — infraestructura filtrada dentro de la entity.
import (
	"database/sql" // persistencia
	"net/http"     // transporte
	"github.com/some/orm"
)
```

Si el paquete de una entity importa `database/sql`, `net/http`, un driver, un cliente HTTP o cualquier lib externa de infraestructura, el dominio se contaminó. La persistencia se expresa con una **interfaz `Store` definida en el punto de uso** que la infraestructura implementa por fuera; el transporte (handlers HTTP, serialización) vive en su propia capa. La entity nunca sabe cómo se guarda ni cómo viaja: solo conoce sus reglas.

El `time` es el caso límite honesto: es stdlib y es tipo de dominio (`createdAt`), así que pasa el test. Si la entity necesitara "la hora actual" de forma testeable, se inyecta un `clock` como dependencia — no se acopla a un reloj de infraestructura.

---

## Checklist

- [ ] La entity es un `struct` con campos **no-exportados** y un **ID de tipo dedicado** (`type UserID int64` o VO de UUID).
- [ ] Todos los métodos usan **pointer receiver**; el value receiver queda para los VOs.
- [ ] La igualdad se compara con `SameAs` (por ID), **nunca** con `==` sobre la entity.
- [ ] El constructor recibe **VOs, no primitivos**, y valida solo reglas **cross-VO**, no cada VO por separado.
- [ ] El estado cambia por **métodos con nombre de dominio** que re-verifican invariantes; cero setters, cero prefijo `Get`.
- [ ] Los accessors existen **solo si hacen falta** y devuelven el **VO**, no su representación interna.
- [ ] La lógica vive **con** los datos (entity rica); nada de `models/` + `services/` anémico.
- [ ] La orquestación que sí sale de la entity es un caso de uso con nombre propio (`change_email.go`), nunca un `UserService`.
- [ ] Los imports pasan el **test de pureza**: sin `database/sql`, `net/http` ni libs externas — solo VOs y tipos de dominio.
