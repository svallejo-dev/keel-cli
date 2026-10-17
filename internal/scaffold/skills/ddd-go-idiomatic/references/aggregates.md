# Aggregates — DDD Táctico al Estilo Idiomático de Go

> **Regla #0 — ante conflicto DDD ↔ Go idiomático, gana Go.** El aggregate no es una
> jerarquía de clases con getters/setters ni un `AggregateRoot<T>` abstracto. Es un
> struct con un límite que **el propio compilador** hace cumplir. Nada de Java-ismos.

Esta referencia trata el **root** y el **límite de consistencia**. La mecánica interna de
la entidad (identidad, ciclo de vida, igualdad) vive en
[`entities.md`](entities.md); los VOs en [`value-objects.md`](value-objects.md); la
transacción y el repositorio en [`application-layer.md`](application-layer.md).

Toolchain: Go 1.26 estándar; suite de verificación fijada a 1.24.7.

---

## 1. Definición

Un **aggregate** es un clúster de entidades y value objects tratado como una sola unidad a
efectos de cambios de datos. Tiene dos propiedades que lo definen:

- Un **límite de consistencia** (consistency boundary): el conjunto de datos que debe estar
  consistente **de forma síncrona** tras cada operación.
- Un **aggregate root**: la entidad que es la **única puerta de entrada válida** al clúster.
  Toda operación de crear/actualizar/borrar sobre los internos pasa por el root.

Regla operativa: **nunca** referencias, cargas o mutas una entidad interna directamente. Si
quieres cambiar una línea de un pedido, se lo pides al pedido, no a la línea. El root es
quien garantiza que ninguna mutación deja el clúster en un estado que viole una invariant.

---

## 2. Realización en Go: la privacidad de paquete ES el límite

En la mayoría de lenguajes OO el límite del aggregate depende de **disciplina**: nada impide
que un `orderService` haga `order.getLines().add(...)` y se salte las invariants. En Go el
límite es **estructural**: las sub-entidades y VOs viven en campos **no-exportados** del
root, y la visibilidad por paquete impide que cualquier código externo los construya o mute.

> Regla → el interior del aggregate va en campos no-exportados; solo métodos exportados del
> root lo tocan. Por qué → convierte "por convención no toques esto" en "el compilador no te
> deja". Es una ventaja real, no un truco de estilo.

`orderLine` no se exporta: fuera del paquete no existe como tipo construible. `lines` es privado,
así que el límite del aggregate es el paquete.

```go
package order

type orderLine struct {
	sku      SKU
	quantity Quantity
	unit     Money
}

// Order es el aggregate root del pedido y sus líneas.
type Order struct {
	id         OrderID
	customerID CustomerID
	lines      []orderLine
}

func NewOrder(id OrderID, customer CustomerID) *Order {
	return &Order{id: id, customerID: customer}
}
```

Las mutaciones pasan por métodos del root que mantienen la invariant. Aquí: cantidad
positiva y **sin SKU duplicado**, con un `Total()` que siempre deriva del estado real.

```go
var (
	ErrDuplicateLine = errors.New("order: SKU ya presente en el pedido")
	ErrLineNotFound  = errors.New("order: SKU no presente en el pedido")
	ErrNonPositive   = errors.New("order: la cantidad debe ser > 0")
)

func (o *Order) AddLine(sku SKU, qty Quantity, unit Money) error {
	if qty <= 0 {
		return ErrNonPositive
	}
	for _, l := range o.lines {
		if l.sku == sku {
			return fmt.Errorf("%w: %s", ErrDuplicateLine, sku)
		}
	}
	o.lines = append(o.lines, orderLine{sku: sku, quantity: qty, unit: unit})
	return nil
}

func (o *Order) RemoveLine(sku SKU) error {
	for i, l := range o.lines {
		if l.sku == sku {
			o.lines = append(o.lines[:i], o.lines[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrLineNotFound, sku)
}

// Total se deriva de las líneas; nunca se guarda en un campo que pueda desincronizarse.
func (o *Order) Total() Money {
	var total Money
	for _, l := range o.lines {
		total = total.Add(l.subtotal())
	}
	return total
}
```

No hay `AggregateRoot` base ni marker interface. `Order` es un struct normal cuya única
peculiaridad es que **posee sus internos y solo los expone tras aplicar reglas**.

---

## 3. Un aggregate por transacción; referencia a otros por ID

El límite de consistencia define la **frontera transaccional**: dentro del aggregate,
consistencia **fuerte** y síncrona; fuera, consistencia **eventual**. De ahí la regla
operativa: **una transacción modifica exactamente un aggregate**. Si una operación necesita
cambiar dos aggregates atómicamente, casi siempre el límite está mal trazado o el segundo
cambio debe ser un evento posterior. El patrón concreto de repositorio y `tx` está en
[`application-layer.md`](application-layer.md); aquí basta la regla.

Corolario en el modelo: referencia a **otros** aggregates **por ID**, nunca por puntero.

```go
// BAD — puntero al otro aggregate: arrastra su grafo y difumina el límite.
type Order struct {
	customer *Customer
}

// GOOD — solo el identificador: el límite de Order queda nítido.
type Order struct {
	customerID CustomerID
}
```

Por qué importa:

- Un puntero invita a `order.customer.Address.Update(...)` → estás mutando el aggregate
  `Customer` dentro de una transacción de `Order`. Viola "un aggregate por transacción".
- Cargar `Order` cargaría `Customer`, y `Customer` sus pedidos, y así el grafo entero.
- El ID hace explícito que `Customer` es **otra** unidad de consistencia: si necesitas sus
  datos, los cargas por su propio root en su propia operación.

| Dentro del límite | Fuera del límite |
|---|---|
| Consistencia fuerte, síncrona | Consistencia eventual |
| Referencia directa (campo interno) | Referencia por ID |
| Una transacción | Coordinación vía eventos / otro use case |

---

## 4. Invariants (dominio) vs autorización (aplicación)

Matiz que se equivoca a menudo: el aggregate impone **invariants de negocio** ("un pedido no
puede tener dos líneas del mismo SKU", "el total nunca es negativo"). **No** decide **quién**
puede ejecutar la operación. La **autorización** (¿este actor puede hacer esto?) es una
preocupación de la capa de aplicación y vive en el **use case**, no en el dominio.

> Regla → el dominio valida el *qué* (estado válido); el use case valida el *quién* (permiso).
> Por qué → el aggregate no conoce sesiones, roles ni tenants, y meterlos dentro lo acopla a
> infraestructura y lo hace intestable de forma aislada.

El caso de uso es un tipo propio en `add_order_line.go`. Primero decide el *quién*
(`actor.CanEditOrder`), después delega el *qué* al root (`o.AddLine`) y guarda un único aggregate.

```go
// internal/order/add_order_line.go
package order

type AddOrderLineInput struct {
	Actor    Actor
	OrderID  OrderID
	SKU      SKU
	Quantity Quantity
	Unit     Money
}

type AddOrderLineOutput struct {
	Total Money
}

// AddOrderLine añade una línea a un pedido que el actor puede editar.
type AddOrderLine struct {
	orders OrderRepository
}

func NewAddOrderLine(orders OrderRepository) *AddOrderLine {
	return &AddOrderLine{orders: orders}
}

func (uc *AddOrderLine) Execute(ctx context.Context, in AddOrderLineInput) (AddOrderLineOutput, error) {
	if !in.Actor.CanEditOrder(in.OrderID) {
		return AddOrderLineOutput{}, ErrForbidden
	}
	o, err := uc.orders.Load(ctx, in.OrderID)
	if err != nil {
		return AddOrderLineOutput{}, err
	}
	if err := o.AddLine(in.SKU, in.Quantity, in.Unit); err != nil {
		return AddOrderLineOutput{}, err
	}
	if err := uc.orders.Save(ctx, o); err != nil {
		return AddOrderLineOutput{}, err
	}
	return AddOrderLineOutput{Total: o.Total()}, nil
}
```

`Order.AddLine` no recibe `actor` ni consulta permisos. Si algún día te ves pasando un
`userID` a un método del dominio para decidir permisos, la lógica está en la capa equivocada.

---

## 5. No exponer el interior mutable

Devolver el slice interno directamente es un footgun: el caller obtiene un alias del backing
array y puede mutar el aggregate **saltándose** las invariants del root. Devuelve siempre una
**copia** o una **vista de solo lectura**.

```go
// BAD — el caller recibe el slice interno y puede mutar sin pasar por AddLine.
func (o *Order) Lines() []orderLine { return o.lines }
```

Con eso basta un `o.Lines()[0].quantity = -5` para corromper el total y romper la invariant sin
un solo error.

```go
// GOOD — proyección inmutable; mutar el resultado no afecta al aggregate.
type LineView struct {
	SKU      SKU
	Quantity Quantity
	Unit     Money
}

func (o *Order) Lines() []LineView {
	out := make([]LineView, len(o.lines))
	for i, l := range o.lines {
		out[i] = LineView{SKU: l.sku, Quantity: l.quantity, Unit: l.unit}
	}
	return out
}
```

Notas idiomáticas:

- Devolver `[]orderLine` (tipo no-exportado) desde un método exportado ni siquiera es útil
  fuera del paquete y `revive`/`golint` lo marcan. La `LineView` exportada es la salida.
- Copiar un slice pequeño de VOs es barato. No optimices prematuramente cediendo el interior.
- Si el consumo es solo lectura secuencial (Go 1.23+), un `iter.Seq[LineView]` es una
  alternativa idiomática que tampoco expone el backing array.

---

## 6. Evita el aggregate prematuro

DDD trae la tentación de sembrar "roots" por todas partes. En servicios pequeños, la mayoría
de los "aggregates" son **una sola entidad** sin sub-entidades: no hay clúster que proteger,
así que no hay root que inventar. Un struct con sus métodos y su constructor validador ya es
suficiente.

> Regla → el límite del aggregate lo define una **invariant transaccional real** que abarca
> varias entidades. Sin esa invariant, no hay aggregate: hay una entidad. Por qué → un root
> ceremonial añade indirección (`AddX`/`RemoveX`, vistas, copias) sin proteger nada.

Señales de que **sí** necesitas un aggregate con root:

- Existe una regla que debe cumplirse mirando **varias** entidades a la vez ("el total de las
  líneas ≤ límite de crédito", "no dos líneas con el mismo SKU").
- Esas entidades **solo** tienen sentido dentro del padre y se crean/borran con él.
- Quieres cargarlas y guardarlas como **una** unidad transaccional.

Si nada de esto aplica, modela una entidad plana. Puedes promoverla a aggregate más tarde: en
Go el refactor es mecánico (haz privados los campos, mueve las mutaciones a métodos). Los
antipatrones asociados (root anémico, aggregate-gigante, invariants filtradas al use case)
están catalogados en [`antipatterns.md`](antipatterns.md).

---

## Checklist

- [ ] El root es la **única** puerta de entrada: las sub-entidades viven en campos no-exportados.
- [ ] Toda mutación del interior pasa por un método del root que mantiene la invariant.
- [ ] Las invariants derivadas (p. ej. `Total()`) se **calculan**, no se guardan en un campo mutable.
- [ ] Otros aggregates se referencian **por ID** (`customerID CustomerID`), nunca por puntero.
- [ ] Una transacción modifica **un** aggregate; lo demás es consistencia eventual.
- [ ] La **autorización** vive en el caso de uso (`add_order_line.go` → `AddOrderLine`); el dominio solo impone invariants de negocio.
- [ ] Los métodos de lectura devuelven **copia o vista**, nunca el slice/map interno.
- [ ] No inventaste un root sin una invariant transaccional real que lo justifique.
