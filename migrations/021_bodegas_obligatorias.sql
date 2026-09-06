-- +goose Up
-- Las bodegas de una cuenta dejan de ser opcionales.
--
-- El comentario original decía «sin filas = todos los almacenes, que es la
-- decisión vigente de MDV». En la práctica nadie llegó a asignar nunca una
-- bodega —no había pantalla ni endpoint— y las cuentas publicaban la suma de
-- los siete almacenes, con Muestras, Garantías y las tres bodegas de
-- consignación de Falabella dentro: WooCommerce vendía unidades que estaban
-- en el centro de distribución de Falabella. Desde ahora una cuenta sin filas
-- aquí no publica nada (store.ErrCuentaSinBodegas) hasta que se le asignen
-- desde «Cuentas». No cambia el esquema: solo deja de mentir el comentario.

COMMENT ON TABLE channel_account_warehouses IS
    'Almacenes que alimentan cada cuenta. Sin filas la cuenta NO publica: '
    'hay que asignarlas desde la pantalla de Cuentas.';

-- +goose Down
COMMENT ON TABLE channel_account_warehouses IS
    'Almacenes que alimentan cada cuenta. Sin filas = todos los almacenes.';
