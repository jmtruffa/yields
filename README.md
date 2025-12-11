Yields API
==========

API en Go (Gin) para cálculos de bonos: yield, precio, paridad, valor técnico, cronograma de pagos y carga masiva vía CSV. Los bonos y flujos viven en PostgreSQL; el coeficiente CER se carga también desde la base. `/apr` está deprecado (usar `/yield`, que maneja cero cupón).

Requisitos
----------
- Go 1.20+
- PostgreSQL (variables: `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DB`)
- `GIN_MODE` (`debug` o `release`). Default: release.
- Opcional: `BOND_SEED_FROM_JSON=1` para seed inicial desde `bonds.json`.

Convenciones de días (`day_count_conv`)
---------------------------------------
1 = 30/360  
2 = Actual/365  
3 = Actual/Actual  
4 = Actual/360  

Endpoints
---------

### GET /yield
Calcula YTM dado un precio. Maneja bonos indexados (CER) y cero cupón.  
Parámetros (query):
- `ticker` (string, requerido)
- `settlementDate` (YYYY-MM-DD, requerido)
- `price` (float64, requerido)
- `initialFee`, `endingFee` (float64, opcionales)
- `extendIndex` (float64, opcional, tasa anual para extrapolar CER)

Devuelve: `Yield`, `MDuration`, `AccrualDays`, `CurrentCoupon`, `Residual`, `AccruedInterest`, `TechnicalValue`, `Parity`, `LastCoupon`, `LastAmort`, CER usado y `Maturity`.

### GET /price
Calcula precio dado una tasa.  
Parámetros (query):
- `ticker` (string, requerido)
- `settlementDate` (YYYY-MM-DD, requerido)
- `rate` (float64, requerido)
- `initialFee`, `endingFee` (float64, opcionales)
- `extendIndex` (float64, opcional)

Devuelve: `Price`, `MDuration`, métricas extendidas (paridad, devengado, residual, CER usado, etc.).

### GET /schedule
Exporta bonos y flujos en ZIP (bonds.csv y cashflows.csv).  
Parámetros (query):
- `ticker` (repetible, requerido; ej. `?ticker=AL30&ticker=GD29D`)
- `settlementDate` (opcional): si se pasa, solo flujos con fecha >= cutoff.

Comportamiento:
- Si un ticker no existe, se incluye en header `X-Missing-Tickers` y se continúa con los demás (si ninguno existe, 404).
- Respuesta: ZIP con `bonds.csv` y `cashflows.csv`.

### POST /upload
Carga/actualiza bonos y flujos desde CSV (multipart). Requiere API key (`X-API-Key`).  
Campos multipart:
- `bonds` (CSV)
- `cashflows` (CSV)

Formato de `bonds.csv` (encabezados):
```
ticker,issue_date,maturity,coupon,index,offset,day_count_conv,active,operation
```
`operation`: `insert` (default) o `update`. Si el ticker existe y no es `update`, se descarta ese bono y sus flujos.

Formato de `cashflows.csv` (encabezados):
```
ticker,date,rate,amort,residual,amount
```

Reglas/validaciones:
- Tickers normalizados a mayúsculas.
- Cada bono debe tener >=1 cashflow; no se aceptan cashflows huérfanos.
- Flujos se ordenan por fecha ascendente y se reasigna `seq`.
- `residual` no puede aumentar (igual o decreciente). `amount` no se valida por ahora.
- Fechas válidas y dentro de [issue_date, maturity].
- day_count_conv debe ser 1..4.
- Procesa bono a bono (parcial): los inválidos se omiten; los válidos se upsert dentro de transacción.
- Tras éxito (insert/update) se recarga la caché en memoria.

Respuesta (`application/json`):
```
{
  "inserted": N,
  "updated": M,
  "skipped": K,
  "errors": ["..."],          // motivos de errores o skips
  "missing_cashflows": ["..."]// bonos sin flujos
}
```

### GET /bonds
Devuelve la lista de tickers disponibles.

### /apr (deprecado)
Usar `/yield`; `/apr` responde 410 con aviso de deprecación.

Autenticación (para /upload)
----------------------------
Tabla `yields_api_keys` (ya creada). Enviar header `X-API-Key`. La API valida `active=true` y actualiza `last_used_at`.

CSV de ejemplo
--------------

`bonds.csv`
```
ticker,issue_date,maturity,coupon,index,offset,day_count_conv,active,operation
AL29N,2020-01-01,2029-01-01,0.09,,0,1,true,insert
```

`cashflows.csv`
```
ticker,date,rate,amort,residual,amount
AL29N,2024-07-01,0.045,5,95,9.75
AL29N,2025-07-01,0.045,5,90,9.55
AL29N,2026-07-01,0.045,5,85,9.35
```

Ejemplos curl
-------------

`/upload`:
```bash
curl -X POST http://localhost:8080/upload \
  -H "X-API-Key: TU_API_KEY" \
  -F bonds=@/ruta/bonds.csv\;type=text/csv \
  -F cashflows=@/ruta/cashflows.csv\;type=text/csv
```

`/schedule`:
```bash
curl -X GET "http://localhost:8080/schedule?ticker=AL29N&settlementDate=2025-12-12" -o schedule.zip
```

Notas internas
--------------
- `BOND_SEED_FROM_JSON=1` permite importar `bonds.json` a la DB en el arranque.
- Los datos se cargan en memoria al iniciar y tras un upload exitoso.
- Día de cómputo configurable por bono (`day_count_conv`). CER usa `offset` en días hábiles.
