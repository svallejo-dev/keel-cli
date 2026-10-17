# Bounded Contexts en Go

## Regla #0 — Ante conflicto, gana go-idiomatic

DDD es un conjunto de **heurísticas sobre dónde vive el comportamiento**, no una estructura de
carpetas ni un framework. Cuando una regla táctica de DDD choca con el estilo idiomático de Go, gana
Go: nada de capas `domain/application/infrastructure` de nivel superior, nada de
`AbstractRepositoryFactory`, nada de getters/setters ceremoniales, nada de un paquete `interfaces`
central. Un bounded context en Go es, casi siempre, **un paquete dentro de `internal/`**.

Esta referencia es autocontenida. `senior-go-idiomatic` es una skill compañera opcional, no un
requisito. Los detalles tácticos viven en `references/value-objects.md`, `references/entities.md`,
`references/aggregates.md`, `references/application-layer.md` y `references/antipatterns.md`.

---

## 1. Qué es un bounded context y cómo mapea a Go

Un bounded context es un límite dentro del cual un modelo y su lenguaje son coherentes y sin
ambigüedad. En Go ese límite es **un paquete** (o un clúster pequeño de paquetes) bajo `internal/`.

- `internal/` es la **frontera de privacidad** que impone el compilador: solo lo importa el árbol enraizado en el padre de ese `internal/`.
- El **contexto es el paquete** (`internal/identity`, `internal/billing`): el paquete se llama como el contexto.

El tipo raíz, sus value objects, invariantes e interfaz de persistencia viven juntos: son el mismo
concepto, no tres paquetes técnicos.

```go
// internal/identity/user.go
package identity

// User es la entidad raíz del bounded context identity.
type User struct {
	id    string
	email Email
}

func Register(id string, email Email) *User {
	return &User{id: id, email: email}
}

func (u *User) ID() string   { return u.id }
func (u *User) Email() Email { return u.email }
```

Los campos son no-exportados: el invariante (un `User` siempre tiene un `Email` válido) solo se
puede construir vía `Register`. Ver `references/entities.md` para identidad y ciclo de vida.

---

## 2. Ubiquitous language = el vocabulario exportado del paquete

El ubiquitous language de un contexto **es su API exportada**; el nombre del paquete es el lenguaje
(`identity.Register`, `identity.Email`, `identity.User`). El dominio se lee en los identificadores
exportados, no en documentación externa.

```go
// internal/identity/email.go
package identity

import (
	"fmt"
	"strings"
)

// Email es un value object: se valida al construir y es inmutable.
type Email string

func ParseEmail(raw string) (Email, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "@") {
		return "", fmt.Errorf("identity: email inválido %q", raw)
	}
	return Email(raw), nil
}
```

**Nada de paquetes-cajón.** `util`, `common`, `models`, `types`, `helpers` no tienen ubiquitous
language: no representan ningún concepto del dominio, solo agrupan cosas. Si no puedes nombrar el
paquete con un sustantivo específico del negocio, todavía no has encontrado el contexto.

| Paquete | Por qué falla |
|---|---|
| `models` | ORM thinking; el tipo queda separado de su comportamiento |
| `util` / `common` | Cajón sin frontera; acopla contextos que no deberían tocarse |
| `types` | Capa anémica de datos; nadie es dueño del invariante |
| `service` | "Servicio" no es un concepto; suele ser un God object |

La misma regla baja un nivel, a los **archivos** del contexto: `service.go`, `usecase.go`,
`manager.go` o `helpers.go` no dicen qué hacen. Dentro de un contexto, cada archivo nombra un
concepto (`user.go`, `email.go`) o un caso de uso (`register_user.go`, `login.go`).

Por qué un value object como `Email` evita la primitive obsession: ver `references/value-objects.md`.

---

## 3. Carpeta por CONTEXTO, no por capa

Este es el error central que trasplantan quienes vienen de Java/C#: organizar el árbol por capa
técnica. En Go se organiza por **concepto**.

```
# BAD — capas técnicas de nivel superior (Java-ism)
internal/
├── domain/          ← ¿de qué dominio?
├── application/     ← capa, no concepto
└── infrastructure/  ← el modelo de User vive lejos de su lógica
```

```
# GOOD — un paquete por contexto; dentro, un ARCHIVO por concepto o caso de uso
internal/
├── identity/
│   ├── user.go              ← entidad raíz + constructores
│   ├── email.go             ← value object
│   ├── user_store.go        ← puerto de persistencia compartido (consumer-defined)
│   ├── register_user.go     ← caso de uso RegisterUser
│   ├── login.go             ← caso de uso Login
│   └── change_email.go      ← caso de uso ChangeEmail
└── billing/
    ├── customer.go
    ├── accounts.go          ← puerto hacia identity (consumer-defined)
    └── charge_customer.go   ← caso de uso ChargeCustomer
```

```
# BAD — archivos-cajón dentro del contexto
internal/identity/
├── models.go        ← ¿qué modelos?
├── service.go       ← Login(), Signup(), ChangeEmail()… todo junto
└── handler.go       ← todos los endpoints
```

Un archivo = una intención. Abrir el árbol de `identity` debe bastar para saber qué sabe hacer el
contexto: registrar, iniciar sesión, cambiar el email. Si dos casos de uso comparten archivo
(`Login()` y `Signup()` en `service.go`), sepáralos aunque sean cortos.

Dentro del contexto se separa **por archivo**, no por sub-paquete: el paquete es la unidad de
diseño y los archivos solo organizan la lectura. Anidar sub-paquetes **no** crea jerarquía ni da
acceso a los no-exportados del "padre" — la visibilidad en Go es por paquete, nunca por carpeta.

Extrae un sub-paquete **solo cuando el contexto crece** y aparece un detalle de infra con nombre
propio — típicamente la impl de la persistencia en `internal/identity/pgstore/`. La interfaz vive en
el consumidor (`identity`), la implementación en el sub-paquete: el contexto **no** importa `pgstore`,
es al revés.

```go
// internal/identity/user_store.go
package identity

import "context"

// UserStore es la persistencia de User que comparten los casos de uso del contexto.
type UserStore interface {
	ByID(ctx context.Context, id string) (*User, error)
	Save(ctx context.Context, u *User) error
}
```

Cada caso de uso (`register_user.go`, `change_email.go`) es un tipo propio que coordina agregados y
transacciones: ver `references/application-layer.md`. Layout completo y anti-patrones: `references/antipatterns.md`.

---

## 4. Integración entre contextos

**Nunca importes los internals de otro contexto.** Que dos paquetes compilen juntos no significa que
deban acoplarse. Los contextos se comunican por exactamente tres vías:

1. **Interfaz publicada consumer-defined** — el consumidor declara qué necesita; se satisface al cablear.
2. **Eventos** — el productor publica un domain event; los consumidores se suscriben sin importarlo.
3. **Anti-Corruption Layer (ACL)** — una función/paquete pequeño de traducción en la frontera.

### Consumer-defined interface

`billing` declara lo que necesita del exterior. **No importa `identity`**; declara el contrato con su
propio vocabulario:

```go
// internal/billing/customer.go
package billing

// Customer es el modelo de billing de la persona que paga; no es identity.User.
type Customer struct {
	AccountID string
	Email     string
	Plan      string
}
```

```go
// internal/billing/accounts.go
package billing

import "context"

// Accounts resuelve clientes fuera de billing; se satisface al cablear.
type Accounts interface {
	Lookup(ctx context.Context, accountID string) (Customer, error)
}
```

Un mismo humano es `User` en `identity` y `Customer` en `billing`: cada contexto modela lo que
necesita, y `billing` declara el contrato sin importar `identity`.

### Eventos

Un domain event cruza la frontera con **tipos primitivos/serializables**, nunca con value objects
internos (`identity.Email` no debe filtrarse a otro contexto ni a un bus):

```go
// internal/identity/user_registered.go
package identity

// UserRegistered se publica al dar de alta un usuario.
type UserRegistered struct {
	UserID string
	Email  string
}
```

### Anti-Corruption Layer mínima

El ACL es el **único** lugar que conoce ambos contextos: traduce el modelo de `identity` al de
`billing`, satisface la interfaz consumer-defined y aísla a `billing` de cambios en `identity`.

```go
// internal/acl/identityacl/accounts.go

// Package identityacl traduce identity al modelo de billing.
package identityacl

import (
	"context"

	"shop/internal/billing"
	"shop/internal/identity"
)

// Adapter satisface billing.Accounts leyendo de identity.UserStore.
type Adapter struct {
	users identity.UserStore
}

func New(users identity.UserStore) *Adapter { return &Adapter{users: users} }

var _ billing.Accounts = (*Adapter)(nil)

func (a *Adapter) Lookup(ctx context.Context, accountID string) (billing.Customer, error) {
	u, err := a.users.ByID(ctx, accountID)
	if err != nil {
		return billing.Customer{}, err
	}
	return translate(u), nil
}

func translate(u *identity.User) billing.Customer {
	return billing.Customer{
		AccountID: u.ID(),
		Email:     string(u.Email()),
		Plan:      "free", // identity no sabe de planes; el default lo pone billing
	}
}
```

El wiring (`cmd/`) es el único punto que importa ambos contextos e inyecta el ACL:
`identityacl.New(users)` satisface `billing.Accounts`, que recibe el caso de uso que lo necesita
(`billing.NewChargeCustomer(accounts, ...)`). Ni `identity`
ni `billing` se importan entre sí; solo el wiring y el ACL conocen a los dos.

---

## 5. Cada contexto tiene su propio modelo

`identity.User` **no es** `billing.Customer`: el mismo humano es `User` con `Email` aquí y `Customer`
con `Plan` allá. Un tipo "global" compartido reintroduce `models` y acopla contextos independientes.

- Ningún tipo cruza la frontera tal cual: se **traduce** (ver el `translate` del ACL).
- Un cambio en `identity.User` rompe **solo** el ACL, no todo `billing`. Ese es el propósito.
- Los IDs cruzan como `string`/`uuid`, no como entidad: guarda `AccountID`, no `*identity.User`.

Cuándo un `string` desnudo merece ser un value object propio: ver `references/value-objects.md`.
Cuándo un cúmulo de tipos forma un agregado con raíz única: ver `references/aggregates.md`.

---

## 6. Contexto = paquete, no automáticamente servicio

Un bounded context es un **límite lógico**, no de despliegue. No lo vuelvas microservicio por defecto:
un paquete es gratis de mover y lo verifica el compilador; la red cuesta latencia, versionado y observabilidad.

- **Paquete primero.** `internal/identity` e `internal/billing` en el mismo binario.
- **Separa a servicio solo cuando el límite ya está probado**: interfaz consumer-defined y ACL
  estables, más una razón operativa real (escalado, aislamiento de fallos, equipos distintos).
- Con el ACL bien hecho, promover a servicio es reimplementar el `Adapter` sobre HTTP/gRPC **sin
  tocar** `billing`: `Accounts` no cambia, solo su implementación.

```
Monolito:   billing ──(Accounts)──▶ identityacl.Adapter ──▶ identity.UserStore (in-proc)
Servicio:   billing ──(Accounts)──▶ identityacl.Adapter ──▶ HTTP client ──▶ identity-svc
```

Empezar por servicios ata el diseño a una decisión de infraestructura antes de saber si el límite es
el correcto. Paga ese coste una vez, tarde, y solo tras hacer bien los paquetes.

---

## Checklist

- [ ] Cada bounded context es **un paquete** bajo `internal/` (`internal/identity`, `internal/billing`)
- [ ] El árbol se organiza por **contexto**, no por capa (`domain/application/infrastructure`)
- [ ] Dentro del contexto se separa por **archivo**; sub-paquete solo para infra (`pgstore`) y transporte
- [ ] Cada archivo nombra un concepto (`user.go`) o un caso de uso (`register_user.go`); cero `service.go`, `usecase.go`, `manager.go`, `helpers.go`
- [ ] La API exportada es el **ubiquitous language**; cero paquetes-cajón (`util`, `models`, `types`)
- [ ] Ningún contexto importa los internals de otro; se hablan por interfaz consumer-defined, evento o ACL
- [ ] Las interfaces se definen en el **consumidor**; las implementa un sub-paquete de infra
- [ ] Cada contexto tiene su **propio modelo**; los tipos se **traducen** en la frontera (ACL), no se comparten
- [ ] Los eventos y los IDs cruzan como **primitivos**, nunca como value objects o punteros a entidades
- [ ] Contexto = paquete por defecto; **separar a servicio solo cuando el límite ya está probado**
- [ ] Ante conflicto DDD vs go-idiomatic, **gana go-idiomatic** (Regla #0)
