# CloudOracle explicado para todos 🧙‍♂️

> Una explicación de **todo** el repositorio: qué hace cada cosa, qué función hace qué,
> por qué se tomó cada decisión, las funciones complicadas por dentro, cómo funciona la
> parte de Python, y al final una valoración del nivel de seniority.
>
> Está escrito en dos capas: primero **"como a un niño de 5 años"** (con dibujos mentales
> y analogías) y luego **el detalle técnico** para cuando quieras profundizar.

---

## 1. ¿Qué es esto? (la idea en una frase)

Imagina que la nube (AWS, Google, Azure) es como **un parque de diversiones gigante donde
pagas por cada juego que dejas encendido**, aunque nadie se esté subiendo. Es muy fácil
dejar luces prendidas y gastar dinero sin darte cuenta.

**CloudOracle es un detective del dinero de la nube.** Mira todo lo que tienes encendido,
te dice *"oye, esta máquina lleva una semana prendida y nadie la usa, apágala y ahorras
$40 al mes"*, y luego te lo explica con palabras bonitas como si fueras el jefe de la
empresa.

Hace **tres trabajos** (lo llaman v1, v2 y v3), pero todos giran alrededor de **la misma
idea: el costo de la nube**.

```
                 ┌─────────────────────────────────────────────┐
   AWS · GCP ───▶│  v1  Auditar lo que YA gastas                │──▶ PDF + tablero web
   Azure         │      (mirar, encontrar desperdicio)         │
                 ├─────────────────────────────────────────────┤
   Terraform ───▶│  v2  Predecir lo que un cambio VA a costar   │──▶ comentario en el PR
   (plan)        │      (antes de apretar el botón)            │
                 ├─────────────────────────────────────────────┤
   Preguntas ───▶│  v3  Preguntar en lenguaje normal           │──▶ respuesta en tu idioma
   humanas       │      "¿cuánto gasté en AWS en abril?"       │
                 └─────────────────────────────────────────────┘
```

- **v1** es el *contador* — revisa la factura que ya tienes.
- **v2** es el *adivino* — antes de comprar algo nuevo, te dice cuánto te va a costar.
- **v3** es el *asistente que habla* — le preguntas con palabras normales y te responde.

El proyecto está escrito **principalmente en Go** (~24.000 líneas), con una parte
**en Python** (~4.500 líneas, todo el v3 — el "asistente que habla") y un **tablero web
en React**.

---

## 2. El mapa del tesoro (estructura del repositorio)

Piensa en el repo como una casa con habitaciones. Cada carpeta es una habitación con un
trabajo:

| Carpeta | ELI5 (qué es) | Qué hace de verdad |
|---|---|---|
| `cmd/oracle/` | La **puerta de entrada** | El programa principal (CLI): los comandos `seed`, `analyze`, `report`, `serve`, `pr-check` |
| `internal/cloud/` | Los **exploradores** | Van a AWS/GCP/Azure y traen la lista de cosas que tienes |
| `internal/generator/` | El **inventor** | Hace datos falsos realistas para probar sin gastar dinero real |
| `internal/analyzer/` | El **detective** | Reglas que detectan desperdicio (máquinas ociosas, discos huérfanos…) |
| `internal/pricing/` | La **calculadora de precios** | Pregunta a AWS cuánto cuesta cada cosa |
| `internal/iac/` | El **lector de planos** | Lee el "plano" de Terraform (lo que vas a construir) |
| `internal/diff/` | El **comparador** | Junta todo y dice "esto sube +$389/mes" |
| `internal/github/` | El **cartero** | Pega el comentario en tu Pull Request de GitHub |
| `internal/report/` | El **impresor** | Genera el PDF y los exports CSV/JSON |
| `internal/llm/` | El **traductor a humano** | Habla con la IA (Gemini/Claude/OpenAI) para escribir resúmenes |
| `internal/db/` | La **bodega** | Guarda todo en PostgreSQL |
| `internal/api/` | La **ventanilla** | El servidor HTTP que sirve el tablero y la API v1 |
| `internal/billing/` | El **lector de facturas** | Abstracción para leer costo real (AWS Cost Explorer) o aproximado |
| `internal/config/` | El **portero** | Lee la configuración (variables de entorno) y valida que esté bien |
| `insights-agent/` | El **asistente que habla** (Python) | TODO el v3: el agente de IA, RAG, guardarraíles |
| `web/` | La **cara bonita** | El tablero React + gráficas |

---

## 3. v1 — El contador: auditar lo que ya gastas

### 3.1 Los exploradores (`internal/cloud/`) — el patrón "Estrategia"

**ELI5:** Imagina que tienes tres amigos: uno habla con AWS, otro con Google, otro con
Azure. Todos hablan idiomas distintos, pero tú solo quieres que te traigan **una lista de
cosas**. Así que les das a todos el mismo formulario para rellenar. Tú no necesitas saber
con quién hablaron; solo lees el formulario.

**Técnico:** Todos los proveedores cumplen una sola interfaz con dos métodos:

```go
type CloudProvider interface {
    Name() string
    FetchResources(ctx context.Context) ([]shared.Resource, error)
}
```

Esto se llama **patrón Estrategia**. `SyntheticProvider`, `AWSProvider`, `GCPProvider` y
`AzureProvider` son "estrategias" intercambiables. La función `NewProvider` (en
`factory.go`) elige cuál usar según una variable de entorno. El resto del programa nunca
sabe cuál está activo — solo ve `[]shared.Resource`. Añadir una cuarta nube = un archivo
nuevo + una línea en el `switch`.

**Decisión clave:** El proveedor `synthetic` (datos falsos) cumple la **misma** interfaz.
Por eso puedes correr **todo** el programa sin tener una cuenta de nube ni credenciales —
genial para demos, pruebas y desarrollo local.

#### La función estrella: `FetchResources` (fan-out con errgroup)

**ELI5:** En vez de ir a buscar las máquinas, *luego* los discos, *luego* las bases de
datos (uno por uno, lento), manda a **4 mensajeros a la vez** y espera a que todos vuelvan.
Si un mensajero se pierde (un servicio falla), los otros 3 igual traen sus cosas. No se
cancela toda la misión por uno que tropezó.

**Técnico:** Cada proveedor real lanza sus 4 llamadas (EC2/RDS/EBS/Lambda en AWS) en
goroutines con `golang.org/x/sync/errgroup`. Los detalles inteligentes:

- Cada goroutine tiene **su propio** `context.WithTimeout` (default 30s), así un servicio
  lento solo se afecta a sí mismo, no a los demás.
- **Degradación elegante:** si una llamada falla, se registra con `slog.Warn` y la goroutine
  hace `return nil` (se traga el error *adentro*). ¿Por qué? Porque si devolviera un error a
  errgroup, **cancelaría el contexto y mataría a los otros mensajeros**. Tragar el error =
  "si RDS falla, EC2/EBS/Lambda igual te dan resultados".
- Cada goroutine escribe en **su propio índice** de un slice pre-dimensionado
  (`results[i]`), así **no hace falta mutex** (candado) — son zonas de memoria distintas.

**Por qué importa:** un escaneo de la nube tiene que tolerar fallos parciales. Un servicio
con throttling o sin permisos debe degradar el resultado, no romperlo.

#### El truco de testabilidad: interfaces estrechas

**ELI5:** En vez de pedir prestado el camión de bomberos completo (con 200 botones) solo
para usar la manguera, defines "una cosa que tiene manguera". Así, en las pruebas, le pasas
una manguera de juguete y nadie nota la diferencia.

**Técnico:** El SDK de AWS se esconde tras interfaces mínimas como `ec2APIClient` (solo
`DescribeInstances` + `DescribeVolumes`). El cliente real las cumple "por estructura"
(structural typing de Go). En las pruebas inyectas un *fake* que devuelve datos enlatados —
sin red, sin credenciales. GCP y Azure usan una variante: una interfaz *lister* que
**aplana la paginación** en una sola llamada "dame todo", porque sus SDKs devuelven
iteradores que no se pueden falsear desde fuera.

### 3.2 El detective (`internal/analyzer/`) — reglas como funciones puras

**ELI5:** El detective tiene una **lista de pistas**. Cada pista es una pregunta tipo
*"¿esta máquina lleva más de 7 días encendida y casi nadie la usa?"*. Si la respuesta es
sí, anota un hallazgo. Revisa cada cosa con cada pista y ordena los hallazgos por cuánto
dinero ahorras.

**Técnico:** Una regla es literalmente:

```go
type Rule func(r shared.Resource) *shared.Finding  // devuelve nil si no aplica
```

Una **función pura**: misma entrada → misma salida, sin efectos secundarios. Las 4 reglas
viven en `rules.go`:

| Regla | Qué detecta | Severidad | Ahorro estimado |
|---|---|---|---|
| `checkEC2Idle` | CPU < 5% **y** edad ≥ 7 días | Alta | 100% (apagar) |
| `checkRDSOversized` | Base de datos con CPU < 10% | Media | 50% (bajar un nivel) |
| `checkEBSOrphan` | Disco sin usar (`UsageMetric == 0`) | Alta | 100% (borrar) |
| `checkLambdaOverProvisioned` | Memoria > 1024MB **y** pocas invocaciones | Baja | 30% |

Añadir una regla = escribir la función + registrarla en un slice. Sin interfaces, sin
configuración. La nota de "edad ≥ 7 días" en EC2 evita marcar máquinas recién creadas que
solo están "calentando".

> 💡 **El bug del que el repo aprendió:** la regla de EC2 comparaba `r.Service != "EC2"`
> (mayúsculas) pero los datos guardaban `"ec2"` (minúsculas). La regla pasaba de largo por
> **todas** las máquinas sin marcar ninguna. Lección: los bugs de comparación de strings son
> la fuente #1 de fallos silenciosos en herramientas de nube.

### 3.3 El inventor (`internal/generator/`)

**ELI5:** Para probar el detective necesitas "casos sospechosos". El inventor crea máquinas
falsas pero realistas, y a propósito hace que **~15% estén ociosas** y **~15% de los discos
estén huérfanos**, para que el detective siempre tenga algo que encontrar.

**Técnico:** `GenerateResources` tira un dado (`rand.Float64()`) para la mezcla de tipos
(50% EC2, 20% RDS, 25% EBS, 5% Lambda), con una **distribución bimodal de uso** diseñada
para disparar el analizador. Las fechas de creación se reparten hasta 365 días atrás, así
la regla de "≥ 7 días" tiene casos que pasan y casos que no.

### 3.4 La bodega (`internal/db/`) — PostgreSQL

**ELI5:** Una bodega donde guardas (a) la lista de cosas que tienes y (b) "fotos" del costo
a lo largo del tiempo (snapshots), para poder dibujar la gráfica de "¿estoy gastando más?".

**Técnico, las funciones interesantes:**

- **`InsertResources`** — un *upsert* transaccional: `ON CONFLICT (id) DO UPDATE` refresca
  costo/uso/timestamp, pero **no** toca `created_at` (preserva cuándo se descubrió). Usa el
  idioma canónico de pgx: un `defer rollback` que ignora `pgx.ErrTxClosed` (porque tras un
  `Commit` exitoso la transacción ya está cerrada).
- **`CreateSnapshot`** — agrega los recursos **en memoria** a un mapa `{cuenta, servicio}
  → {conteo, costo}` antes de insertar, para que la tabla de snapshots sea pequeña (una
  fila por cuenta/servicio, no por recurso).
- **`ListTrends`** — la función más astuta del módulo. Si corres 3 escaneos el mismo día,
  no quieres contar el costo 3 veces. Hace una **pasada de deduplicación** que se queda solo
  con el snapshot **más reciente** por (día, servicio), luego suma por día. Devuelve una
  serie de tiempo limpia para la gráfica.

### 3.5 El traductor a humano (`internal/llm/`)

**ELI5:** El detective encuentra los problemas, pero habla en números. El traductor le pide
a una IA (Gemini, Claude u OpenAI) que escriba un resumen ejecutivo bonito para el jefe.

**Técnico:** Una sola interfaz `Provider` con `GenerateSummary`, `GenerateText`, `Name`.
`NewProvider` elige el modelo: si `LLM_PROVIDER` está puesto, ese; si no, **gana la primera
API key disponible** en orden Gemini → Claude → OpenAI; si no hay ninguna, devuelve el
*sentinel* `ErrNoProvider` y el reporte se genera **sin** la sección de IA (degradación
elegante — cualquiera puede clonar el repo y correrlo).

Los 3 clientes están hechos con `net/http` puro (sin SDKs de vendor) para mantener pocas
dependencias. `BuildPrompt` **pre-calcula** totales, severidades y rollups por servicio
para que la IA solo *narre*, no *calcule* (las IAs son malas sumando listas largas).

> ✅ **Bug encontrado y corregido:** en `claude.go` el campo `MaxTokens` usaba el tag JSON
> `"maxTokens"` pero la API de Anthropic espera `"max_tokens"` (snake_case). Tal como estaba,
> la API ignoraba ese campo. Ya está arreglado a `"max_tokens"`.

#### La joya de la corona: el reintentador (`retry.go`)

**ELI5:** Cuando le hablas a la IA y te dice "estoy ocupada, vuelve más tarde" (error 429),
no te rindes: esperas un ratito y vuelves a intentar. Pero esperas un tiempo **aleatorio**
para que no todos los clientes vuelvan al mismo segundo y la saturen otra vez.

**Técnico:** `retryTransport` es un `http.RoundTripper` (se mete **dentro** del transporte
HTTP, no alrededor de cada llamada). Así *todas* las rutas de petición heredan los
reintentos gratis. Lo hábil:

- **Reproduce el cuerpo** de la petición en cada intento (lo bufferea una vez y reinstala
  `req.Body` + `req.GetBody`), porque el transporte consume el body en cada `RoundTrip`.
- **Respeta `Retry-After`** del servidor, en sus **dos formatos** (segundos enteros *o*
  fecha HTTP) — `parseRetryAfter`.
- Si no hay `Retry-After`, usa **backoff exponencial con "full jitter"**: espera un tiempo
  aleatorio uniforme en `[0, baseDelay·2^intento]`. Es el algoritmo recomendado por AWS para
  evitar el "rebaño atronador" (que todos reintenten sincronizados).
- El `select` entre `time.After(delay)` y `ctx.Done()` hace que **cancelar el contexto corte
  el reintento al instante**.

### 3.6 La ventanilla (`internal/api/`) y la cara bonita (`web/`)

**ELI5:** Un servidor web que sirve dos cosas: el tablero bonito (React, va metido dentro
del propio binario de Go con `go:embed`) y la **API v1** que usa el asistente de Python.

**Técnico, lo interesante:**

- **Dos niveles de rutas:** `/api/*` (v0, sin autenticación, para el tablero local) y
  `/api/v1/*` (con `X-API-Key`, para el agente). El middleware se compone de afuera hacia
  adentro: `cors(requestID(logging(mux)))`.
- **`authMiddleware`** usa `subtle.ConstantTimeCompare` para comparar la API key — comparación
  de **tiempo constante** para no filtrar la longitud/prefijo de la clave por *timing*.
- **`requestIDMiddleware`** genera un ID por petición (12 bytes de `crypto/rand` → 24 hex) y
  lo **devuelve en la cabecera de respuesta**, para que puedas citarlo en un reporte de bug.
- **`providerForServiceAccount`** resuelve qué nube es cada servicio. El caso difícil:
  `"functions"` lo usan **GCP y Azure**. El desempate mira la forma del `AccountID`: si es un
  UUID de 36 caracteres con guiones en las posiciones 8 y 13 → Azure, si no → GCP.
- **`static.go`**: si un archivo no existe, sirve `index.html` (soporte de rutas del SPA). Si
  el tablero ni siquiera está compilado, sirve una página amable "corre `npm run build`" en
  vez de un 404.

### 3.7 El lector de facturas (`internal/billing/`) — el corazón conceptual

**ELI5:** Hay dos formas de saber cuánto gastas: (a) **adivinar** a partir de las "fotos"
que tomas cada tanto (aproximado), o (b) **leer la factura real** de AWS. CloudOracle define
"una cosa que me dice los costos" y le puedes enchufar cualquiera de las dos sin cambiar
nada más.

**Técnico:** La interfaz `billing.Source` tiene un método `Costs(ctx, start, end)` que
devuelve registros normalizados `{Provider, Service, AmountUSD}` + un `DataSource` (etiqueta).
Dos implementaciones:

- **`snapshotSource`** (por defecto): convierte la tasa mensual de las snapshots a la duración
  del periodo (`scale = días/30`). Etiqueta: `snapshots_approximation` ("no coincide con la
  factura al centavo").
- **`CostExplorerSource`**: consulta `GetCostAndUsage` de AWS (costo real *unblended*).
  Etiqueta: `billing_aws_cost_explorer`. Un detalle sutil y crítico: Cost Explorer trata el
  `End` como **exclusivo**, así que suma `+1 nanosegundo` (que rueda al día siguiente) para
  que ambas fuentes respondan **idéntico** a la misma pregunta de fechas.

**La gran idea — `data_source`:** cada endpoint v1 estampa una etiqueta distinta
(`snapshots_approximation`, `billing_aws_cost_explorer`, `heuristic_rules`, `live_inventory`)
para que el agente **nunca confunda una aproximación con la verdad** y ponga el aviso correcto.

---

## 4. v2 — El adivino: predecir lo que un cambio va a costar

Esta es la tubería que convierte un **plan de Terraform** en un **comentario de PR con el
costo**. Tres etapas:

```
Plan JSON ──▶ internal/iac (leer el plano) ──▶ internal/pricing (poner precios)
          ──▶ internal/diff (comparar y resumir) ──▶ internal/github (pegar el comentario)
```

### 4.1 El lector de planos (`internal/iac/`)

**ELI5:** Terraform escribe un "plano" de lo que va a construir/cambiar/borrar. Este lector
lo entiende y saca, de cada cosa, los datos que importan para el precio (tipo de máquina,
tamaño de disco, etc.).

**Técnico, la función más astuta — `ResourceChange.Action()`:** Terraform **nunca** dice
literalmente "reemplazar". Lo expresa como un arreglo de dos acciones: `["delete","create"]`
o `["create","delete"]`. Esta función detecta ese par (en cualquier orden) y lo colapsa en
una sola acción sintética `ActionReplace`, para que el resto del código tenga que entender
un solo valor en vez de re-deducir "¿esto es un reemplazo?" en cada lugar.

**Sobre `after_unknown` (hallazgo importante):** la documentación dice que los decodificadores
manejan `after_unknown` (atributos que no se conocen hasta aplicar), pero **el código nunca
parsea ese campo**. El manejo es *implícito*: los helpers (`getString`, `getInt`) tratan el
`null` de JSON igual que "ausente". Si un atributo **requerido** viene null → el extractor
falla → el recurso se marca `Skipped`. O sea, "manejo de `after_unknown`" en realidad es
"null = ausente, y requerido ausente = saltar". La doc lo sobrevende un poco.

Los helpers usan un patrón **tri-estado** `(valor, presente, error)` en vez de `(valor,
error)`, porque a veces quieres aplicar un default *solo si* el atributo está genuinamente
ausente, pero tratar un *tipo equivocado* como error duro. Un retorno de dos valores no puede
expresar esa diferencia.

### 4.2 La calculadora de precios (`internal/pricing/`)

**ELI5:** Para cada cosa del plano, le pregunta a AWS "¿cuánto cuesta esto por hora?" y lo
multiplica por las horas del mes. Guarda las respuestas en un cajón por 7 días para no
preguntar lo mismo una y otra vez (los precios de AWS casi no cambian).

**Técnico, las funciones complicadas:**

- **`Client.GetProducts`** — envuelve la API de Pricing de AWS. Detalle que confunde a todos:
  la región del **endpoint** está fija en `us-east-1` (la API solo se sirve desde ahí), y la
  región del recurso que estás cotizando va como un **filtro**, no como la región del cliente.
  Los filtros se **ordenan por nombre** para que dos llamadas idénticas produzcan bytes
  idénticos (clave para el caché).

- **`parseOnDemandPriceUSD`** — el precio está enterrado bajo
  `terms.OnDemand.<sku>.priceDimensions.<dim>.pricePerUnit.USD`, donde `<sku>` y `<dim>` son
  strings opacos que no conoces de antemano. El truco: como cada producto simple tiene
  **exactamente un** SKU y una dimensión, **ordena las claves y toma la primera**
  (determinista, porque Go aleatoriza el orden de los mapas). Servicios con tarifas por
  tramos (S3, invocaciones de Lambda) quedan fuera de alcance a propósito.

- **El patrón "exactamente un producto":** cada estimador asegura que la API devolvió **un
  solo** producto. Cero → "no encontré precio"; más de uno → "filtro poco específico". Un
  empate se trata como un *bug*, no se resuelve en silencio. Esa es la columna vertebral de
  la corrección.

- **El caché de 7 días (`Cache`)** — envuelve cualquier `productGetter`. La **clave** es
  `sha256(serviceCode + filtros_ordenados)`, y ese hash es el nombre del archivo. Lo astuto:
  - Las escrituras son **atómicas** (escribe a `tmp-*.json` y hace `os.Rename`), así un lector
    concurrente nunca ve un archivo a medio escribir.
  - Archivos corruptos o vencidos **se borran** al detectarlos, para no tropezar con la misma
    basura.
  - Es **best-effort**: cualquier error del caché se traga (se loguea), nunca rompe el camino
    feliz. Sin candados: si dos procesos corren la misma clave, "el último que escribe gana".

- **`EstimateChange`** — convierte un precio en un **delta con signo** según la acción:
  crear = +después; borrar = −antes; actualizar/reemplazar = después − antes; no-op = 0,
  saltado. La parte sutil es el álgebra del *breakdown* (`mergeDeltaBreakdown`): para
  update/replace construye el delta componente por componente, preservando el orden para que
  los *golden tests* (pruebas de salida byte-exacta) sean estables.

- **La confianza (`Confidence`):** cada estimador asigna Alta/Media/Baja según **cuántas
  suposiciones fuertes tuvo que hacer**. EC2 siempre es Baja (asume SO Linux, On-Demand,
  etc.); EBS plano es Media; Lambda es Baja (no modela el costo de invocaciones). Las
  suposiciones se listan en `Notes` para total transparencia.

### 4.3 El comparador (`internal/diff/`)

**ELI5:** Toma todos los precios de cada cosa, los suma, y arma el resumen: "el total sube
+$389/mes, y los que más pesan son estos tres".

**Técnico:**

- **`Analyze`** corre el estimador sobre cada cambio. Si **uno** falla, **no es fatal**: se
  loguea y se convierte en un resultado `Skipped` con la razón. Garantía de diseño: *un
  hipo de la API nunca impide producir un comentario*. Luego ordena por `abs(delta)`
  descendente (orden estable), categoriza cada cambio (Created/Deleted/Updated/Replaced/
  Skipped) y calcula los *top movers* y la confianza agregada (la **más débil** gana).

- **`formatDelta` / `trendEmoji`** usan una **tolerancia de medio centavo** (`0.005`):
  los deltas de punto flotante acumulan ruido, y un "cambio cero" puede caer en
  `$0.0000001`. Tratar todo bajo medio centavo como `$0.00`/⚪ evita un 🔴 por un error de
  redondeo (que haría ver el comentario roto).

- **El renderizador Markdown** pre-formatea **todos** los valores antes de la plantilla, así
  el cuerpo de la plantilla no tiene lógica de formato. El comentario lleva un marcador
  oculto `<!-- cloudoracle-pr-v1 -->` que sirve para **encontrar y actualizar** el propio
  comentario del bot en corridas futuras (en vez de spamear el PR).

#### La narrativa con IA y el respaldo silencioso (`narrative.go`)

**ELI5:** Por defecto el comentario lo escribe una plantilla fija. Si hay IA configurada,
le pide a la IA una narrativa de 1-3 frases. Pero si la IA falla, se queda con la plantilla
**sin que el usuario note nada**. Siempre sale un comentario válido.

**Técnico:** `generateLLMNarrative` es un *guantelete de validación* — cualquier fallo cae
al respaldo: provider nulo, contexto cancelado, error de la IA, respuesta vacía, más de 500
caracteres (ignoró el "1-3 frases")… `cleanNarrative` quita preámbulos como "Aquí está la
narrativa:" o "Claro,".

> 🐛 **El bug que arreglaron (muy ilustrativo):** las advertencias (caveats) eran una lista
> plana, y eso causó que la IA culpara al recurso **RDS** (el principal) por una advertencia
> que en realidad era del **NAT Gateway** ("no se modela el costo de datos"). La solución:
> particionar las advertencias en tres bloques etiquetados — del recurso principal, de
> *otros* recursos (con un encabezado literal "NO atribuir al recurso principal"), y de todo
> el plan. (Visible en el commit `1b1001b`.)

### 4.4 El cartero (`internal/github/`)

**ELI5:** Pega el comentario en tu PR. Pero es listo: si ya pegó uno antes, **lo edita** en
vez de pegar uno nuevo. Así nunca llena el PR de comentarios repetidos.

**Técnico:** `PostOrUpdateComment` hace un *upsert idempotente* basado en el marcador HTML:
lista todos los comentarios (paginado, con tope de 50 páginas), encuentra los suyos por el
marcador, elige el **más recientemente actualizado** (`pickMostRecentMatch`) y hace PATCH; si
no hay ninguno, hace POST. `capBody` recorta a 60KB (por debajo del límite real de GitHub).
Si hay duplicados, **avisa** pero **no borra** (ser conservador con efectos destructivos,
ruidoso con diagnósticos). Los errores se mapean a prefijos de string estables que el CLI usa
para decidir el código de salida.

### 4.5 La orquestación: `oracle pr-check`

**ELI5:** El director de orquesta que junta todo: leer el plano → poner precios → comparar →
renderizar → (opcional) pegar en GitHub. Y devuelve un **código de salida distinto según de
quién fue la culpa**.

**Técnico:** `runPRCheck` usa códigos de salida diferenciados para que la GitHub Action sepa
a quién culpar: `1` = el plan del desarrollador está roto; `2` = falló nuestra dependencia de
precios (AWS, transitorio); `3` = no pudimos escribir la salida; `4` = falló el post a GitHub.
`pr-check` se despacha **antes** de conectar a la base de datos, porque es una transformación
sin estado (plan → markdown) y corre en la Action donde no hay PostgreSQL.

---

## 5. v3 — El asistente que habla (la parte de **Python**) 🐍

Esta es la parte que más te interesa. Vive en `insights-agent/`, está escrita en **Python
3.12** con **LangChain/LangGraph**, y usa **Gemini** (`gemini-2.5-flash`). Es el "asistente
que habla": le preguntas en lenguaje normal y te responde.

> **¿Por qué Python y no Go?** Porque las librerías de agentes de IA (LangChain, LangGraph,
> RAG) viven en Python. La decisión fue: mantener el **servidor Go como una API de datos
> limpia y bien probada**, y poner **el razonamiento y la IA en Python**. Las dos mitades
> hablan por HTTP.

### 5.1 El flujo completo de una pregunta

```
"¿Cuánto gasté en AWS en abril?"
  → GeminiAgentRunner.ask()           (runtime.py — arma todo una sola vez)
    → run_guarded()                   (guardrails/runner.py — la red de seguridad)
      → ask_supervisor(graph)         (graph/supervisor.py — el cerebro multi-agente)
      → validate_answer()             (guardrails/validation.py — ¿los números son reales?)
      → deterministic_answer()        (guardrails/fallback.py — si algo falló, respuesta honesta)
```

### 5.2 El cerebro multi-agente: el supervisor (`graph/supervisor.py`)

**ELI5:** Imagina una oficina. Hay un **jefe** (supervisor) que recibe tu pregunta y decide
a qué **especialista** mandársela:

- 🧮 el **analista de costos** (sabe consultar "¿cuánto gasté?", "¿en qué servicio?",
  "¿está subiendo?", "¿qué tengo?"),
- 💰 el **asesor de ahorros** (sabe "¿dónde puedo ahorrar?"),
- 📚 el **experto en conceptos** (sabe "¿qué es rightsizing?" buscando en una biblioteca).

El especialista hace su trabajo, vuelve con **un solo resumen**, y el jefe decide si necesita
otro especialista o si ya puede mandar la respuesta a un **redactor** (synthesizer) que
escribe la respuesta final en tu idioma. Hay un **límite de vueltas** para que el jefe no se
quede dando vueltas para siempre.

**Lo más importante:** el equipo **no usó** la función pre-hecha de LangGraph
(`create_react_agent`). La **escribieron a mano** (un `StateGraph` propio), porque querían
control preciso sobre los presupuestos, los límites y qué cuenta como "evidencia". La versión
pre-hecha (`graph/basic.py`) se quedó solo para pruebas y comparación.

**Técnico, las piezas:**

- **`SupervisorState`** — el "pizarrón compartido" (un `TypedDict`). Lo astuto son los
  *reducers*: `messages` usa `add_messages` (acumula), `tool_calls` y `observations` usan
  `operator.add` (concatena). Por eso cada nodo puede devolver un diccionario parcial chiquito
  en vez de reconstruir todo el estado, y aun así el contador global de llamadas a
  herramientas (para el presupuesto) queda correcto.

- **`supervisor(state)`** — el nodo que enruta. Le da a la IA unas "herramientas de
  enrutamiento" (una por especialista + `finish`) y **lee cuál herramienta llamó la IA** —
  ese nombre es el siguiente destino. Ojo: el supervisor **nunca ejecuta** esa herramienta
  (tienen cuerpo `_noop`); solo le importa *cuál* eligió. ¿Por qué enrutar por llamada a
  herramienta y no con `with_structured_output`? Para que el mismo "modelo falso" de las
  pruebas pueda manejar tanto al supervisor como a los trabajadores con un solo mecanismo.

- **`decide(state)`** — la función de arista condicional donde **los topes de costo ganan
  sobre los deseos del modelo**: si `hops > max_hops` o `len(tool_calls) >= max_tool_calls`,
  fuerza ir a `synthesize` sin importar qué quería el modelo. Así, un modelo confundido o
  con *prompt injection* que nunca llama a `finish` **igual termina**, y el usuario igual
  recibe una respuesta con lo que se haya juntado.

#### La función que reemplaza a `create_react_agent`: `_run_react`

**ELI5:** Es el "bucle de pensar-actuar" de cada especialista. Funciona así: el especialista
piensa → ¿necesito una herramienta? → la usa → mira el resultado → vuelve a pensar →
¿necesito otra? … hasta que tiene la respuesta o se le acaban los intentos.

**Técnico:** Un bucle `for _ in range(max_iters)`:
1. Le pregunta al modelo (con sus herramientas atadas).
2. ¿No pidió herramientas? → **listo**, devuelve su texto como respuesta final.
3. ¿Pidió herramientas? → por cada una: chequea el **presupuesto** (si se pasó, le devuelve
   "presupuesto agotado, responde con lo que tengas"), la ejecuta, y mete el resultado como
   un `ToolMessage` para que la siguiente vuelta el modelo lo vea.

Detalles sutiles muy bien pensados:
- **Dos topes distintos:** `max_iters` limita las *vueltas con el modelo*; `tool_budget`
  limita las *ejecuciones de herramientas* (derivado del presupuesto global compartido).
- **Señales de control ≠ datos:** los mensajes de "presupuesto agotado" o "herramienta
  desconocida" se le dan al modelo como `ToolMessage`, pero **NO** entran a la lista de
  `observations` (la evidencia). Comentario en el código: *"solo las salidas reales de
  herramientas son evidencia de grounding"*. Esto mantiene honesta la validación posterior.
- **Los errores se vuelven observaciones, no crashes** (las herramientas tienen
  `handle_tool_error=True`).

**¿Por qué los trabajadores guardan su "ruido" local?** Un trabajador puede hacer varias
llamadas internas, pero contribuye **un solo** `AIMessage` resumen al pizarrón compartido.
Así el supervisor y el redactor ven una conversación limpia (una pregunta, un hallazgo por
especialista) en vez de docenas de mensajes crudos. Las llamadas crudas igual fluyen a las
listas laterales (`tool_calls`, `observations`) para el presupuesto y el grounding.

#### El redactor: `synthesize` (decisión muy no-obvia)

**ELI5:** Junta los hallazgos de los especialistas y escribe la respuesta final.

**Técnico:** El truco está en que los hallazgos de los trabajadores se le pasan al redactor
**como material dentro de un turno *humano*, no como turnos de asistente previos.** ¿Por qué?
Porque si se los pasara como historial del asistente, el modelo creería que "la respuesta ya
se dio" y solo añadiría una coletilla — **soltando los números**. Al reformular los hallazgos
como un *briefing* que el usuario le da, el redactor reproduce las cifras de forma confiable.
El prompt además exige: mismo idioma que el usuario, no inventar números, mostrar los avisos
de `data_source` y citar la base de conocimiento.

### 5.3 Los guardarraíles: la red de seguridad (`guardrails/`)

Esta es, técnicamente, **la parte más madura y diferenciadora del proyecto**. Es lo que
separa un demo de juguete de algo "de producción".

#### `run_guarded` — el único punto de entrada (`runner.py`)

**ELI5:** Envuelve toda la corrida del agente con una red de seguridad. Si el agente se cae,
o si su respuesta no es de fiar, te da una **respuesta honesta con los datos crudos** en vez
de una mentira bonita o un error feo.

**Técnico:** Tres etapas, dos modos de fallo manejados por separado (porque tienen datos
distintos disponibles):
1. **Correr** — `ask_supervisor` envuelto en `try/except`. Si la corrida explota (cuota,
   timeout, bug) → respaldo determinista **sin** observaciones.
2. **Validar** — si `validate=True`, llama a `validate_answer`. Si la respuesta no es válida
   → respaldo, pero esta vez **con** las observaciones (para mostrar los datos crudos).
3. **Pasar** — devuelve la respuesta real.

#### El anti-alucinaciones: *grounding* determinista (`validation.py`) — la pieza más astuta

**ELI5:** Antes de creerle a la IA, revisa que **cada cifra en dólares que dijo aparezca de
verdad en los datos que trajeron las herramientas**. Si la IA dice "$150" pero ninguna
herramienta devolvió algo cercano a 150, casi seguro se lo inventó → se rechaza. Y esto se
hace con matemática pura, **sin gastar otra llamada a la IA**.

**Técnico, el algoritmo paso a paso:**
1. **Extraer las cifras de la respuesta** — dos regex capturan `$1,234.56` y `200 USD`. Solo
   se extraen números **anclados a moneda** (un "5%" o "10 instancias" no se trata como
   afirmación monetaria → evita falsas alarmas).
2. **Si no hay cifras → pasa gratis** (una respuesta conceptual no tiene nada que inventar).
3. **Construir el pajar** — concatena **todos** los números de **todas** las observaciones.
   Asimetría intencional: a la respuesta se le buscan solo *montos*, pero al pajar se le
   aceptan *cualquier número* (porque una herramienta devuelve `{"total_usd": 150.0}` donde
   `150` no está anclado a `$` en el JSON).
4. **Emparejar con tolerancia** — `tol = max(0.01, |cifra|·0.01)`, o sea **1% de la cifra,
   con piso de un centavo**. Absorbe redondeos: la IA dice "$150" y la herramienta devolvió
   `149.99`. Una cifra está *grounded* si algún número del pajar cae dentro de la tolerancia.
5. **Veredicto** — si queda alguna cifra sin emparejar → falla duro.

Esto atrapa el tipo de alucinación más dañino (un monto equivocado), gratis y de forma
determinista. Lo que **no** atrapa es un número correcto con **atribución equivocada** (el
número bien, pero del proveedor/servicio/periodo equivocado) — para eso está el juez.

#### El juez LLM — segunda opinión, solo cuando hace falta

**ELI5:** Si la respuesta pasó el chequeo barato **pero contiene números**, una segunda IA
("el juez") la revisa buscando errores más sutiles (número real pero mal atribuido). El juez
solo puede **rechazar**, nunca rescatar.

**Técnico:** El juez se dispara **solo si** (a) hay cifras numéricas **y** (b) se configuró un
modelo juez. Su decisión es **fail-open**: solo una respuesta que empieza con `FAIL` rechaza;
**cualquier otra cosa — incluso una respuesta vacía o corrupta — pasa**. Decisión deliberada:
un juez malogrado no debe bloquear una buena respuesta. Aprieta la precisión sin volverse un
nuevo punto de fallo.

#### El respaldo honesto (`fallback.py`)

**ELI5:** Cuando no se puede confiar en la IA, en vez de inventar, dice: *"No pude darte una
respuesta verificada. Razón: X. Aquí están los datos crudos que trajeron las herramientas,
verifícalos tú mismo"* y los muestra. Honestidad por encima de fluidez.

**Técnico:** Compone la respuesta **sin ninguna llamada a IA** (puro texto de los datos que ya
tiene, así está garantizado que termina y no inventa números). Recorta cada salida a 600
caracteres. Si no hay observaciones (rama de crash), da un mensaje operativo: revisa que el
servidor esté accesible y la API key.

#### Los topes de costo (`RunLimits`)

**ELI5:** Tres límites para que una pregunta no pueda gastar dinero sin control: máximo de
decisiones del jefe (6), máximo de llamadas a herramientas (8), máximo de vueltas por
especialista (6). Un bucle confundido o inyectado no puede gastar de más.

### 5.4 Las herramientas (`tools/cloudoracle.py`)

**ELI5:** Las "manos" del agente. Cada herramienta es una llamada HTTP a un endpoint del
servidor Go. El agente elige cuál usar leyendo la **descripción** de cada una.

**Técnico:**
- **`CloudOracleClient`** — cliente `httpx` async que pone la cabecera `X-API-Key` y genera
  un `X-Request-ID` por petición (`secrets.token_hex(12)` = 24 hex, **igual** que el lado
  Go) para correlacionar logs entre Python y Go sin esfuerzo manual. Valida fechas y
  proveedores **antes** de la llamada HTTP. Errores tipados: `CloudOracleTransportError`
  (red) y `CloudOracleAPIError` (con un `code` legible por máquina).
- **`build_tools`** envuelve los 5 métodos como `StructuredTool`. **Lo importante son las
  descripciones (docstrings):** son largas y guían al modelo a elegir bien — *"esto NO es una
  consulta de gasto; para 'cuánto gasté' usa cloudoracle_cost_summary"*. El equipo
  deliberadamente mantiene el **system prompt corto** y mete la guía de dominio **en las
  descripciones de las herramientas** (lección aprendida: los prompts largos "se desvían del
  comportamiento real del modelo").
- **`ToolException` + `handle_tool_error=True`** — cada error se vuelve una **observación
  que el modelo lee y de la que se recupera**, en vez de abortar la corrida o (peor) inventar
  números de un diccionario vacío.

### 5.5 RAG: la biblioteca de FinOps (`rag/` + `tools/knowledge.py`)

**ELI5:** Para preguntas conceptuales ("¿qué es rightsizing?", "¿debería comprar reservas?")
el agente no consulta números: busca en una **biblioteca** de notas de FinOps. Esto se llama
RAG (búsqueda + generación). Es **opcional** (solo se enciende si configuras `DATABASE_URL`).

**Técnico, la tubería:**
- **`corpus.py`** — carga 5 notas markdown empaquetadas y las **trocea** con un
  `RecursiveCharacterTextSplitter` (chunk 1000, solape 150). Lo astuto: los separadores van
  en orden `["\n## ", "\n### ", "\n\n", "\n", " ", ""]`, así trocea **primero por estructura
  markdown** (encabezados), no por ventanas arbitrarias — cada trozo tiende a ser una sección
  coherente, lo que mejora la relevancia. Cada documento guarda metadata `{source, title}`
  (eso es lo que luego se vuelve **cita**).
- **`embeddings.py`** — convierte texto en vectores con Gemini (`text-embedding-004`). El
  diseño **espeja** el ABC del LLM: igual que `LLMProvider`, hay un `EmbeddingsProvider`
  (ABC) con una sola implementación Gemini. Añadir otro backend es puramente aditivo.
- **`store.py`** — envuelve **pgvector** (extensión de PostgreSQL para búsqueda por
  similitud). `build_retriever` hace una búsqueda *top-k* (default 4).
- **`ingest.py`** — el CLI `insights-agent-ingest` que embebe el corpus a la base.
- **`knowledge.py`** — la herramienta `finops_knowledge_search`. Formatea cada documento como
  `[source: archivo — título]` + texto → así el modelo **cita la fuente**.

**Qué hay en la biblioteca:** 5 notas — `rightsizing.md`, `commitment-discounts.md`
(Reserved Instances / Savings Plans / CUDs), `cost-allocation-and-tagging.md`,
`finops-glossary.md`, y `data-sources-and-caveats.md` (esta última es la que deja al agente
responder "¿qué tan exactos son estos números?" con citas, no adivinando).

### 5.6 La cara HTTP del agente (`api/app.py`) y el runtime (`runtime.py`)

**ELI5:** Además del CLI, el agente corre como servidor (FastAPI) con `POST /ask` y
`GET /health`. CLI y servidor comparten **exactamente el mismo cerebro**, así se comportan
idéntico.

**Técnico:**
- **`GeminiAgentRunner`** (runtime.py) arma todo **una sola vez** (modelo + cliente + tools +
  grafo + límites) y expone `ask()`, que en realidad delega a `run_guarded` (o sea, el
  público siempre obtiene la versión con red de seguridad). La herramienta de RAG solo se
  añade si `database_url` está configurado, con los imports pesados **diferidos** adentro de
  la función para no cargar el stack de DB cuando no se usa.
- **FastAPI** construye el runner una vez en el *lifespan* y lo reutiliza entre peticiones
  (es caro de construir). Depende de un `Protocol` estructural (`ask()` + `aclose()`), no de
  la clase concreta, así las pruebas inyectan un runner falso sin Gemini ni servidor Go.

---

## 6. Cómo se prueba todo (la calidad invisible)

**ELI5:** Hay **469 pruebas unitarias + 21 de integración**. Lo impresionante: las pruebas
**nunca** tocan la red real, ni AWS, ni Gemini, ni una base de datos de verdad (salvo las de
integración, que levantan un PostgreSQL en Docker).

**Técnico:**
- **Go:** cada proveedor de nube se prueba con *fakes* del SDK (paginación, errores por
  servicio, degradación). El LLM se prueba con un `RoundTripper` mockeado. Las pruebas de
  integración usan **testcontainers-go** (PostgreSQL 16 real en Docker), comparten **un**
  contenedor y limpian con `TRUNCATE … RESTART IDENTITY CASCADE` entre pruebas (toda la suite
  en ~5s en vez de ~60s).
- **Python:** un `ScriptedChatModel` (subclase de `BaseChatModel`) reproduce secuencias de
  mensajes escritas a mano, y `pytest-httpx` mockea los endpoints Go. Así ambos grafos corren
  de forma **determinista**. El RAG se prueba offline con un `InMemoryVectorStore` +
  `DeterministicFakeEmbedding` — sin pgvector, sin API de embeddings.

> 💡 **Nota Windows (de la memoria del proyecto):** unos *golden tests* byte-exactos fallaban
> en Windows porque Git convertía los saltos de línea a CRLF. Se arregló forzando LF con
> `.gitattributes` (`eol=lf`).

---

## 7. Catálogo rápido: "¿qué función hace qué?" (las complicadas)

| Función | Dónde | Qué hace (en una línea) |
|---|---|---|
| `FetchResources` | `cloud/*_provider.go` | Trae recursos de 4 servicios en paralelo, tolerando fallos parciales |
| `Analyze` (analyzer) | `analyzer/analyzer.go` | Corre todas las reglas y ordena hallazgos por ahorro |
| `ListTrends` | `db/trends.go` | Dedup "último snapshot por día/servicio" → serie de tiempo |
| `retryTransport.RoundTrip` | `llm/retry.go` | Reintentos con backoff + jitter, respeta `Retry-After`, reproduce el body |
| `computeDelay` | `llm/retry.go` | Calcula la espera (full jitter o `Retry-After`) |
| `authMiddleware` | `api/middleware.go` | Compara la API key en tiempo constante |
| `providerForServiceAccount` | `api/cost_handlers.go` | Desempata GCP vs Azure mirando la forma del AccountID |
| `CostExplorerSource.Costs` | `billing/cost_explorer.go` | Lee costo real de AWS, normaliza el límite de fecha exclusivo |
| `ResourceChange.Action()` | `iac/terraform.go` | Colapsa el `[delete,create]` de Terraform en un `Replace` |
| `parseOnDemandPriceUSD` | `pricing/parse.go` | Saca el precio de un JSON con claves opacas (ordena y toma la 1ª) |
| `Cache.GetProducts` | `pricing/cache.go` | Caché de 7 días, escritura atómica, auto-limpieza |
| `EstimateChange` | `pricing/change.go` | Precio → delta con signo según la acción |
| `mergeDeltaBreakdown` | `pricing/change.go` | Álgebra de breakdown para update/replace |
| `Analyze` (diff) | `diff/engine.go` | Agrega estimaciones → CostDiff (top movers, neto, confianza) |
| `generateLLMNarrative` | `diff/narrative.go` | Narrativa IA con guantelete de validación + respaldo |
| `PostOrUpdateComment` | `github/comments.go` | Upsert idempotente del comentario por marcador |
| `runPRCheck` | `cmd/oracle/main.go` | Orquesta pr-check con códigos de salida diferenciados |
| `_run_react` 🐍 | `graph/supervisor.py` | Bucle ReAct a mano (el reemplazo de `create_react_agent`) |
| `decide` 🐍 | `graph/supervisor.py` | Arista que fuerza síntesis cuando se agota el presupuesto |
| `synthesize` 🐍 | `graph/supervisor.py` | Redacta la respuesta final (findings como briefing humano) |
| `run_guarded` 🐍 | `guardrails/runner.py` | Une correr → validar → respaldar |
| `deterministic_grounding` 🐍 | `guardrails/validation.py` | Cada cifra $ debe existir en las observaciones (±1%) |
| `chunk_documents` 🐍 | `rag/corpus.py` | Trocea markdown por estructura (encabezados primero) |

---

## 8. Las grandes decisiones de diseño (y por qué)

1. **Reglas deterministas primero, IA después.** El 80% del desperdicio se detecta con
   funciones puras (predecibles, gratis, instantáneas). La IA se reserva para lo que es buena:
   convertir datos en prosa ejecutiva. Invertir el orden sería lento, caro y poco fiable.

2. **Solo lectura, a propósito.** CloudOracle *analiza y reporta*, no *actúa* (no apaga ni
   borra nada). Es más seguro de adoptar y quita la objeción "¿esta herramienta acaba de
   borrar mi base de datos?" en la compra.

3. **Interfaces sobre herencia.** Tanto los proveedores de nube como los de LLM son
   interfaces mínimas. Añadir uno nuevo = un archivo nuevo, cero cambios en lo existente.
   Es la tipificación estructural de Go en su mejor forma.

4. **Configuración central que falla rápido.** `config.Load()` lee todas las variables una
   vez y **acumula todos los errores** en uno solo legible (no falla en el primero). Ningún
   componente llama a `os.Getenv` por su cuenta.

5. **Degradación elegante en todas partes.** Sin IA → reporte sin resumen. Servicio caído →
   los demás siguen. Caché roto → cliente directo. Cost Explorer falla al iniciar → cae a
   snapshots. Siempre WARN-y-continúa, nunca abortar.

6. **Honestidad de procedencia (`data_source`).** Cada número lleva una etiqueta que dice si
   es factura real, aproximación de snapshot o heurística. El agente nunca confunde una
   aproximación con la verdad.

7. **La frontera Go/Python.** Go = API de datos pequeña y muy probada. Python = razonamiento
   e IA (donde viven LangChain/RAG). Se hablan por HTTP. Cada lado hace lo que mejor hace.

8. **Determinismo para poder probar.** Filtros ordenados, claves ordenadas, orden estable,
   modelos "falsos" con guiones escritos a mano. Todo para que el caché y los *golden tests*
   sean byte-estables y la IA sea testeable offline.

---

## 9. Cosas honestas (lo que no es perfecto)

Para que la valoración sea justa, lo que un revisor crítico anotaría:

- **~~Bug en `claude.go`~~ (corregido):** el tag JSON era `"maxTokens"` cuando la API de
  Anthropic espera `"max_tokens"`; ya está arreglado.
- **~~La doc sobrevendía `after_unknown`~~ (corregido):** `README.md` y `docs/architecture.md`
  ahora describen el manejo real (null/unknown = ausente → requerido ausente se salta).
- **Sin candados en el caché de precios** ni *ledger* de migraciones — decisiones
  conscientes y documentadas (best-effort / esquema de 2 archivos), pero límites a tener en
  cuenta si el proyecto crece.
- **El agente no tiene memoria conversacional** ni streaming ni otros providers (Anthropic/
  OpenAI) en Python todavía — listados explícitamente como "lo que aún no está".

Que estas limitaciones estén **documentadas y sean decisiones conscientes** (no descuidos)
es, en sí mismo, una señal de madurez.

---

## 10. Veredicto: ¿qué nivel de seniority? 🎓

**Nivel: Senior sólido, con tramos que rozan Staff.** Si esto fuera una entrevista o un
portafolio, lo ubicaría entre **Senior (L5) y Staff (L6)**.

### Por qué Senior (lo que está claramente por encima de "intermedio"):

| Señal | Evidencia en el repo |
|---|---|
| **Arquitectura con patrones bien aplicados** | Estrategia (providers), Factory, interfaces estrechas para testabilidad, funcional-options, abstracción `billing.Source` |
| **Concurrencia correcta** | `errgroup` con timeouts por servicio, slices pre-dimensionados sin mutex, degradación elegante, propagación de cancelación por contexto |
| **Resiliencia de producción** | Retry como `RoundTripper` con full-jitter + `Retry-After`, caché atómico best-effort, fallbacks por todas partes |
| **Disciplina de testing real** | 469+21 pruebas, fakes de SDK, testcontainers, modelos LLM "scripted", RAG offline — todo sin tocar la red |
| **Multi-lenguaje con criterio** | No metió todo en un lenguaje: Go para la API de datos, Python para la IA, frontera HTTP clara |
| **Documentación que explica el *por qué*** | Los docs no solo dicen qué hace el código, sino qué alternativas se descartaron y por qué |

### Por qué roza Staff (lo que va más allá del "buen senior"):

- **Los guardarraíles del agente de IA.** El grounding determinista (atrapar alucinaciones
  con matemática pura, gratis, antes de gastar en un juez LLM), el fallback honesto, los
  topes de costo que ganan sobre el modelo. Esto es pensamiento de "cómo opero esto en
  producción sin que me cueste una fortuna o mienta", que es nivel Staff.
- **El agente multi-agente escrito a mano.** Reemplazar `create_react_agent` con un
  `StateGraph` propio *por razones articuladas* (control de presupuesto, separación de
  evidencia, drivability en tests) — no por capricho — es señal de dominio profundo del
  framework, no solo de uso.
- **Decisiones de costo/seguridad sutiles.** Comparación en tiempo constante de la API key,
  `Retry-After` en dos formatos, el ajuste de ±1 nanosegundo en Cost Explorer, la tolerancia
  de medio centavo para no mostrar 🔴 por redondeo. Son detalles que solo se ven con
  experiencia de producción.

### Por qué *aún no* es claramente Staff/Principal:

- Es un proyecto de un solo autor; no hay evidencia del trabajo "de Staff" de *alinear a
  varios equipos* o *definir estándares organizacionales*.
- Quedan asperezas menores (el bug de `maxTokens`, la doc de `after_unknown`) que un proceso
  de revisión de un equipo grande habría atrapado.
- Algunas decisiones "simples a propósito" (sin ledger de migraciones, sin candados de caché)
  son correctas para este tamaño, pero el salto a Staff suele implicar haber operado sistemas
  donde esas simplificaciones ya no alcanzan.

### En una frase

> Este repositorio lo escribió alguien que **no solo sabe hacer que funcione, sino que sabe
> hacer que funcione *en producción*, que sea *barato de operar*, que sea *testeable sin la
> nube*, y que *explica por qué* tomó cada decisión.** Eso es un ingeniero **Senior fuerte**,
> y la parte del agente de IA (guardarraíles, multi-agente a mano, grounding determinista)
> es trabajo de **nivel Staff**. La mezcla Go+Python+React, hecha con criterio y no por
> moda, refuerza esa lectura.

---

*Documento generado a partir de un análisis función-por-función de los ~24.000 renglones de
Go y ~4.500 de Python del repositorio. Para el detalle de cada subsistema, ver también
`docs/architecture.md`, `docs/v3-guide.md` e `insights-agent/README.md`.*
