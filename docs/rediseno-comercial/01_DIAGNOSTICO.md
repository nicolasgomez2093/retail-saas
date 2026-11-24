# Diagnóstico orientado al producto

Propuesta de Etapa A, pendiente de aprobación. Inspección estática del repositorio actual. No se ejecutaron recorridos de usuario ni se midió usabilidad. Las fricciones se deducen de la composición de pantallas y deben contrastarse en la revisión del prototipo.

La capacidad comercial supera la claridad de su presentación. Catalog.tsx contiene edición, variantes, herramientas y consulta, mientras Pricing e Inventory mantienen tareas necesarias para terminar el mismo producto. El comerciante debe coordinar esas operaciones. La prioridad es ofrecer un espacio de trabajo por producto con resultados parciales explícitos y acciones en contexto.

La primera entrega debe permitir crear una remera, definir variantes, cargar sus precios y registrar mercadería mediante una operación válida. Cada bloque conserva su propio resultado y permite retomar lo incompleto. Una etiqueta de preparación nunca garantiza una futura venta ni reemplaza las validaciones del POS.

La propuesta evita un asistente rígido obligatorio. Una ficha progresiva permite regresar a cualquier sección sin completar imágenes o clasificación opcionales. El alta inicial solicita nombre y guarda un producto real, que puede existir sin variantes.

Evidencia principal
- apps/web/src/features/catalog/Catalog.tsx, estados form/detail y herramientas, formularios y validación por campo.
- apps/web/src/app/routes.ts, paths y resolveRoute.
- apps/api/internal/modules/catalog/catalog.go, ProductInput, ProductChanges y Detail.
- apps/api/internal/platform/httpapi/pricing.go, configuración, asignación y precio por variante.
- apps/api/internal/modules/inventory/existences.go, agregación de saldos y elegibilidad de ubicación.
- docs/design/retail-moderno/GAP_ANALYSIS.md y NAVIGATION_MAP.md (retirados del repositorio el 23/11/2026; copia en ~/Documentos/RespaldosRetail/Retail-Limpieza-20261123-231630).

El documento anterior de navegación describe aliases como destinos. El router actual establece rutas canónicas inventario/existencias y productos/precios. La nueva propuesta conservará enlaces antiguos durante una transición aprobada.

La calidad actual no se cuantifica con una puntuación inventada. La revisión comercial deberá observar tiempo, errores y saltos de pantalla para preparar un producto y cobrarlo.
