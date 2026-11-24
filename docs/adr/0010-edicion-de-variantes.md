# ADR 0010 — Edición y estado de variantes

El propietario puede corregir SKU, talle, color, códigos y estado de una variante. Su UUID y las referencias de precios se conservan. Los SKU siguen siendo únicos por comercio sin distinguir mayúsculas. Las variantes existentes empiezan activas al aplicar la migración.

Un reemplazo de códigos usa el primer elemento como principal y guarda los secundarios ordenados. Un arreglo vacío libera todos los códigos; omitir el campo conserva los actuales. Los cambios de atributos y códigos se ejecutan en una transacción. Si un código pertenece a otra variante del mismo comercio, se revierte toda la edición.

Una variante inactiva conserva su SKU, códigos y precios. El propietario puede verla en la ficha y reactivarla. El personal ve variantes activas en el detalle y al buscar por SKU. La búsqueda por código y la consulta operativa de precios exigen que tanto la variante como el producto estén activos.
