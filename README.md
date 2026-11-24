# Retail Platform

Base de un SaaS de gestión comercial multiempresa y multisucursal. Esta entrega incluye el entorno local y el flujo de identidad, sesión y sucursales asignadas.

## Arranque local

Requiere Docker con Compose v2 o posterior. Para las verificaciones ejecutadas fuera de Docker, instalá Go según `apps/api/go.mod`, Node 22 y Make.

```bash
cp .env.example .env
make up
make seed
```

Abrí http://localhost:5173 e ingresá con el correo `demo@example.test` y la contraseña que elegiste al ejecutar `make seed`. El comando crea el comercio de demostración una sola vez y falla si ya existe. Usá los valores de ejemplo para desarrollo. Los puertos publicados se limitan a loopback. `make down` detiene el entorno y conserva los datos locales.

```bash
curl -fsS http://localhost:8080/api/health/live
curl -fsS http://localhost:8080/api/health/ready
```

Compose aplica las migraciones mediante un servicio de una sola ejecución antes de iniciar la API. `make migrate` permite ejecutarlas explícitamente. Cada migración se registra con su checksum. Para modificar el esquema, agregá un archivo SQL numerado.

El servidor Vite de desarrollo usa un proxy del mismo origen para `/api`. El build de producción incluye el manifiesto PWA y registra el service worker bajo HTTPS o localhost. Para servirlo, configurá el mismo origen para el frontend y `/api` y usá `COOKIE_SECURE=true` con un `WEB_ORIGIN` HTTPS. La autenticación requiere conexión a la API.

## Alta operada de un comercio

Con Compose iniciado y usando Bash, ejecutá el comando y elegí una contraseña de al menos 12 bytes. El comando devuelve los UUID del comercio, del propietario y de la primera sucursal.

```bash
read -rsp "Contraseña del propietario: " retail_owner_password
printf '\n'
printf '%s\n' "$retail_owner_password" | docker compose run --rm -T api provision-tenant \
  --slug tienda-norte --name "Tienda Norte" \
  --timezone America/Argentina/Tucuman --currency ARS \
  --store-code CENTRAL --store-name "Sucursal Central" \
  --owner-email titular@example.test --owner-name "Titular"
unset retail_owner_password
```

Ingresá desde la web con el correo y la contraseña elegidos. Si esas credenciales coinciden en varios comercios, podés elegir uno después de iniciar sesión. Si el slug ya existe, el comando devuelve un error de conflicto y conserva el alta anterior. Para ejecutar el binario directamente, usá una base migrada y configurá `DATABASE_URL`.

## Administración de usuarios

La pantalla **Usuarios** aparece para propietarios y encargados autorizados. Permite dar de alta personal y editar nombre, estado y sucursales. El propietario administra personal en todo su comercio. El encargado administra cajeros y operadores cuyas asignaciones estén dentro de sus sucursales activas. La desactivación y el cambio de rol cierran las sesiones de la persona. Los propietarios pueden cambiar el rol del personal entre cajero, operador de inventario y encargado. [Alcance y auditoría](docs/adr/0004-administracion-de-usuarios.md).

## Configuración de sucursales

Los propietarios pueden crear sucursales y editar su código, nombre, domicilio y estado desde **Administrar sucursales**. Una sucursal nueva queda asignada al propietario que la crea. Al desactivarla se conservan todas las asignaciones y se suspende su acceso operativo. [Decisión técnica](docs/adr/0005-sucursales-y-roles.md).

## Catálogo

Abrí **Catálogo** para buscar por nombre, SKU o código de barras y combinar filtros por categoría y marca. Los propietarios pueden crear y renombrar categorías y marcas, asignarlas a productos y agregar variantes. Al crear o editar una variante, cargá un código por línea y ubicá el principal primero. Los ceros iniciales se conservan. El propietario también puede desactivar o reactivar variantes desde su ficha. El buscador acepta un lector en modo teclado que envíe Enter. [Decisiones del catálogo](docs/adr/0007-catalogo-inicial.md).

### Fotos de productos

En la ficha del producto abrí **Fotos** y subí una imagen JPEG o PNG. Podés guardar hasta seis fotos, cambiar la principal o quitar una foto. La interfaz prepara imágenes de hasta 1600 píxeles por lado y admite archivos de origen de hasta 12 MiB. [Almacenamiento y límites de la API](docs/adr/0013-fotos-de-productos.md).

### Importar y exportar CSV

En **Catálogo**, abrí **Importar y exportar CSV**, descargá la plantilla y conservá la fila de encabezados. El archivo debe estar en UTF-8 y puede usar coma o punto y coma. Usá hasta 1000 filas de datos y 2 MiB por archivo. El CSV crea productos y variantes. Si alguna fila falla, se cancela toda la carga. [Columnas, agrupación y ejemplos](docs/adr/0012-catalogo-csv.md).

### Etiquetas

En el detalle de una variante activa o en el resultado de escanearla, abrí **Etiqueta de …**, elegí el código y la cantidad de copias. En el cuadro de impresión, usá papel de 60 × 40 mm, escala del 100 % y desactivá los encabezados y pies. También podés guardar un PDF. [Formato y validación](docs/adr/0011-etiquetas-code128.md).

## Listas de precios

En **Listas de precios**, el propietario puede crear listas, asignarlas a sucursales activas y cargar o quitar el precio de cada variante. Para compartir precios, asigná la misma lista a todas las sucursales que deban usarla. Tras buscar un código en **Catálogo**, elegí una de tus sucursales para ver el importe. Si la sucursal carece de lista o de un precio para esa variante, la pantalla indica **Sin precio**. [Modelo y alcance](docs/adr/0008-listas-de-precios.md).

## Inventario

Desde **Inventario**, elegí una sucursal y una ubicación. El propietario puede crear salones y depósitos en sus sucursales asignadas. Para registrar un ajuste, buscá la variante, ingresá unidades positivas para un ingreso o negativas para un egreso y agregá un motivo. La pantalla muestra stock físico, reservado y disponible, junto con los últimos movimientos. [Permisos e integridad de stock](docs/adr/0014-inventario-inicial.md).

### Stock mínimo

En **Inventario**, seleccioná una variante para configurar o quitar su mínimo en la ubicación elegida. La alerta aparece cuando el disponible llega a ese valor o queda por debajo. Activá **Solo con alerta de stock mínimo** para revisar qué artículos necesitan atención. [Reglas y permisos](docs/adr/0017-stock-minimo.md).

### Conteos físicos

En **Inventario**, elegí una ubicación y abrí **Conteos físicos**. Seleccioná las variantes, abrí el conteo e ingresá las unidades físicas encontradas. Confirmar registra las diferencias y actualiza las existencias. Si cambió el saldo entre la apertura y la confirmación, cancelá el conteo y abrí uno nuevo. [Reglas de conteo](docs/adr/0016-conteos-fisicos.md).

### Transferencias

En **Inventario → Transferencias**, elegí la sucursal de origen, las ubicaciones y las variantes para crear un borrador. Desde el origen podés despacharlo o cancelarlo. Para recibir, elegí la sucursal de destino y confirmá las unidades realmente recibidas. Si hay faltantes, ingresá un motivo. [Estados, permisos y reglas de stock](docs/adr/0015-transferencias-de-stock.md).

## Caja

En **Caja**, elegí la sucursal y una caja. El propietario puede dar de alta cajas en sus sucursales asignadas. Abrí una sesión con el efectivo inicial, registrá ingresos o egresos con motivo y cerrala declarando el efectivo contado. El arqueo guarda el efectivo esperado y la diferencia. [Importes, permisos y arqueo](docs/adr/0018-caja-inicial.md).

## Punto de venta

Abrí una caja, asegurate de tener una ubicación vendible con stock y una lista de precios asignada a la sucursal. En **Punto de venta**, buscá o escaneá las variantes, indicá cantidades y elegí el medio de pago. Para combinar medios, usá **Pago mixto** y repartí el total entre los importes. Si hay efectivo, indicá cuánto recibiste para calcular el vuelto. La selección registra cómo pagó el cliente al momento de vender. La confirmación devuelve el número de venta y el desglose de pagos. Si el resultado de una confirmación es incierto, usá **Reintentar confirmación** en la misma pantalla. [Reglas de confirmación](docs/adr/0019-venta-en-efectivo.md) y [registro de medios de pago](docs/adr/0021-medios-de-pago.md).

### Venta fiada

Para vender **Fiado**, elegí el cliente de la sucursal o registralo desde la venta. Si paga una parte, ingresá el anticipo por medio de pago. La confirmación descuenta las prendas y registra el saldo a cobrar. Abrí **Fiados pendientes** para ver los saldos de la sucursal según tus permisos. [Reglas del fiado](docs/adr/0022-fiado-en-el-punto-de-venta.md).

Para cobrar después, abrí **Ver abonos** en el fiado, elegí una caja abierta, indicá el medio y el importe aplicado. Podés abonar una parte o el total del saldo. Una vez saldado, el historial sigue visible con el filtro **Saldados**. Si hay un error de conexión durante el registro, usá **Reintentar abono** en la misma pantalla. [Registro de abonos](docs/adr/0023-abonos-de-fiados.md).

### Reservas

En **Reservas → Nueva reserva**, elegí un cliente y una ubicación vendible, y agregá las prendas por código de barras o búsqueda. Podés registrar la reserva con la caja cerrada. Para entregarla, elegí **Llevar al carrito** y confirmá el cobro en una caja abierta. Si necesitás liberar las unidades, usá **Cancelar reserva** o **Registrar vencimiento** después de la fecha elegida. [Reglas de reservas](docs/adr/0024-reservas-de-mercaderia.md).

Para guardar una seña, abrí la reserva activa y elegí **Registrar seña**. Indicá la caja, el medio y el importe. Al entregar, el carrito muestra la seña y el saldo a cobrar. En una cancelación o vencimiento, la seña queda visible como pendiente de resolver. [Reglas de señas](docs/adr/0025-senas-de-reservas.md).

### Ticket interno e historial

Después de confirmar la venta, abrí **Ver ticket**. Para una venta anterior, abrí **Historial de ventas** en el Punto de venta, buscá el número y elegí **Reimprimir ticket**. La vista previa admite anchos de 58 y 80 mm. Elegí el mismo ancho en el cuadro de impresión y desactivá sus encabezados y pies. También podés guardar un PDF. [Datos históricos y alcance](docs/adr/0020-ticket-interno.md).

### Recepción de devoluciones

En **Historial de ventas → Devoluciones de venta**, elegí las cantidades recibidas, el motivo y el destino de las prendas. Para cuarentena, usá una ubicación no vendible de la sucursal. El historial muestra el importe de referencia calculado con el precio original. [Reglas de recepción](docs/adr/0026-recepcion-de-devoluciones.md).

Para una venta pagada íntegramente sin fiado, usá **Registrar reintegro** desde la recepción y elegí una caja abierta. Si hubo una seña aplicada, su importe cuenta como parte del pago original. Cuando hay varios medios, indicá cuánto reintegraste por cada uno hasta cubrir el importe de la devolución. Solo la parte en efectivo genera un egreso en esa sesión. Registrá los reintegros electrónicos después de realizarlos por el canal correspondiente. [Reglas de reintegros con seña](docs/adr/0030-reintegros-con-sena-aplicada.md).

En una venta fiada, elegí **Compensar deuda** desde la recepción. Indicá el importe a aplicar al saldo pendiente. El ajuste figura en el historial de la devolución y del fiado y se puede registrar con la caja cerrada. Si queda una diferencia y los cobros previos la cubren, usá **Registrar reintegro**. El sistema tiene en cuenta los anticipos, las señas aplicadas y los abonos según el medio original. [Reglas de reintegros de fiados](docs/adr/0032-reintegros-de-fiados.md).

Para un cambio de igual importe en una venta pagada, elegí **Registrar cambio** desde la recepción. Seleccioná las prendas de reemplazo y su ubicación de salida. El sistema valida el precio vigente y la disponibilidad. El cambio queda vinculado a la devolución original. Si el reemplazo cuesta más, elegí una caja abierta y el medio para registrar la diferencia. Para combinar medios, elegí **Pago mixto** e ingresá los importes. El vuelto se calcula sobre la parte en efectivo. Si el reemplazo cuesta menos, registrá la diferencia reintegrada por los medios originales. [Reglas de cambios con diferencia a favor del cliente](docs/adr/0036-cambios-con-diferencia-a-reintegrar.md).

Un fiado admite cambios desde una recepción pendiente. Si el reemplazo tiene igual valor, podés registrar el cambio con la caja cerrada. Si cuesta más, registrá el cobro de la diferencia en una caja abierta. Si cuesta menos, ingresá cuánto aplicar al saldo del fiado y reintegrá el resto por los medios originales. Si compensás toda la diferencia, podés hacerlo con la caja cerrada. [Reglas para cambios de fiados de menor valor](docs/adr/0039-cambios-de-fiado-de-menor-valor.md).

Desde el historial de un cambio, elegí **Devolver reemplazo** para registrar cuáles de las prendas entregadas vuelven al local. Elegí el destino vendible o de cuarentena e indicá el motivo. El historial muestra las cantidades recibidas de ese cambio y el valor histórico del reemplazo. [Recepción de prendas de reemplazo](docs/adr/0040-recepcion-de-prendas-de-reemplazo.md).

Si se devuelve un reemplazo, elegí **Registrar otro cambio** desde esa recepción. Si las prendas nuevas suman el mismo importe de referencia, podés confirmar con la caja cerrada. Si cuestan más, registrá la diferencia en una caja abierta con uno o varios medios. Si cuestan menos, reintegrá la diferencia por los medios disponibles de la venta y de la cadena de cambios. En fiados podés aplicar parte o toda la diferencia al saldo pendiente. Si la compensás completa, podés confirmar con la caja cerrada. [Otro cambio de menor valor en fiados](docs/adr/0044-otro-cambio-de-menor-valor-en-fiados.md).

Para resolver la devolución de un reemplazo sin entregar otra prenda, elegí **Registrar reintegro**. En un fiado también podés elegir **Compensar deuda** antes de reintegrar el resto. La compensación puede registrarse con caja cerrada. [Devolución directa de un reemplazo](docs/adr/0045-devolucion-directa-de-un-reemplazo.md).

### Proveedores

En **Proveedores**, el propietario puede dar de alta, buscar y editar proveedores. El nombre es obligatorio y el correo y el teléfono son opcionales. Los encargados con sucursales activas asignadas pueden consultar proveedores activos. [Alcance del padrón](docs/adr/0046-proveedores-del-comercio.md).

En **Órdenes de compra**, seleccioná una sucursal y un proveedor activo. Agregá las variantes por nombre, SKU o código de barras e indicá las cantidades. El costo por unidad es opcional. Las órdenes se consultan desde la sucursal en la que se registraron. [Reglas de órdenes de compra](docs/adr/0047-ordenes-de-compra.md).

Abrí una orden y elegí **Registrar recepción**. Seleccioná una ubicación e indicá las unidades aceptadas y rechazadas. Solo las aceptadas ingresan al stock. La orden muestra lo pendiente y el historial de recepciones con sus observaciones. [Recepciones parciales de compra](docs/adr/0048-recepciones-parciales-de-compra.md).

Al recibir unidades aceptadas, podés informar su costo unitario efectivo o copiar el de la orden de forma explícita. Si dejás el campo vacío, el historial muestra **Sin costo informado**. [Costos de recepción](docs/adr/0049-costo-unitario-de-recepcion.md).

## Verificación

```bash
npm ci --prefix apps/web
make check
TEST_DATABASE_URL='postgres://retail:retail_local_only@127.0.0.1:55432/retail?sslmode=disable' make test-db
docker compose config --quiet
npx --yes @redocly/cli@1.34.5 lint docs/openapi.yaml --extends minimal
```

Ajustá la URL de pruebas si cambiaste los valores de `.env`. Usá una base de desarrollo o de CI. La prueba de integración crea y elimina su propio esquema y requiere permisos para crearlo. `make test` ejecuta la integración si se configuró `TEST_DATABASE_URL`.

## Estructura

- `apps/api/cmd/retail` compone los servicios y expone los comandos.
- `apps/api/internal/modules` contiene autenticación y lectura de sucursales.
- `apps/api/internal/platform` contiene configuración y transporte HTTP.
- `apps/api/migrations` contiene el SQL versionado y su ejecutor.
- `apps/web` contiene la interfaz React y los assets de la PWA.
- `infra/docker` y `compose.yaml` definen el entorno local.
- `docs` reúne el [contrato API](docs/openapi.yaml), las [decisiones](docs/adr/) y las [preguntas abiertas](docs/open-questions.md).
