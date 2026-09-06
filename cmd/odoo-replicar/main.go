// Command odoo-replicar copia el catálogo de una instancia de Odoo a otra.
//
// Sirve para tener en el Odoo local de pruebas el catálogo real del cliente,
// que es la única forma de probar la publicación con datos que se parezcan a
// los de producción: nombres largos de verdad, SKU con el formato real,
// categorías con su profundidad y stock repartido entre varias bodegas.
//
// IMPORTANTE — esto NO es una restauración de copia de seguridad:
//
//   · Copia DATOS (categorías, productos, almacenes, existencias), no la
//     instalación. Los módulos, la configuración contable, los usuarios y los
//     permisos no viajan.
//   · El origen suele ser Odoo Enterprise y el destino Community, así que los
//     campos de módulos Enterprise —como l10n_co_edi_brand, donde MDV guarda
//     la marca— no existen en el destino y se omiten. No es una pérdida real:
//     desde el cambio de propiedad de datos, Integra ya no lee la marca de
//     Odoo.
//   · Es idempotente por referencia interna (default_code): volver a
//     ejecutarlo actualiza en vez de duplicar.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/mdv/integra/internal/odoo"
)

func main() {
	origen := odoo.Config{
		URL:      os.Getenv("ORIGEN_URL"),
		Database: os.Getenv("ORIGEN_DB"),
		Username: os.Getenv("ORIGEN_USER"),
		APIKey:   os.Getenv("ORIGEN_API_KEY"),
	}
	destino := odoo.Config{
		URL:      env("DESTINO_URL", "http://localhost:8069"),
		Database: env("DESTINO_DB", "integra_pruebas"),
		Username: env("DESTINO_USER", "admin"),
		APIKey:   env("DESTINO_API_KEY", "admin"),
	}

	if origen.URL == "" || origen.Database == "" || origen.Username == "" || origen.APIKey == "" {
		fmt.Fprint(os.Stderr, `Faltan las credenciales del origen.

  ORIGEN_URL       https://mi-empresa.odoo.com
  ORIGEN_DB        el nombre REAL de la base
  ORIGEN_USER      usuario
  ORIGEN_API_KEY   clave de API

El nombre real de la base casi nunca coincide con el subdominio. Para
averiguarlo: entra a Odoo en el navegador, abre la consola (F12) y escribe
odoo.info — devuelve { server_version: "...", db: "NOMBRE-REAL" }.

Destino (opcional, por defecto el Odoo local de pruebas):
  DESTINO_URL, DESTINO_DB, DESTINO_USER, DESTINO_API_KEY
`)
		os.Exit(2)
	}

	if err := replicar(origen, destino); err != nil {
		fmt.Fprintf(os.Stderr, "\n✗ %v\n", err)
		os.Exit(1)
	}
}

func replicar(origen, destino odoo.Config) error {
	fmt.Printf("Origen  %s (%s)\n", origen.URL, origen.Database)
	fmt.Printf("Destino %s (%s)\n\n", destino.URL, destino.Database)

	src, err := odoo.Connect(origen)
	if err != nil {
		return fmt.Errorf("conectando al origen: %w", err)
	}
	dst, err := odoo.Connect(destino)
	if err != nil {
		return fmt.Errorf("conectando al destino: %w", err)
	}
	fmt.Printf("✓ Autenticado en ambas instancias\n\n")

	// La compañía va primero: la moneda decide en qué se valoran los pedidos
	// que Integra cree después, y cambiarla con asientos ya creados es mucho
	// más difícil que hacerlo con la base recién nacida.
	if err := replicarCompania(src, dst); err != nil {
		return err
	}
	if err := replicarImpuestos(src, dst); err != nil {
		return err
	}
	if err := replicarTarifas(src, dst); err != nil {
		return err
	}

	cats, err := replicarCategorias(src, dst)
	if err != nil {
		return err
	}
	almacenes, err := replicarAlmacenes(src, dst)
	if err != nil {
		return err
	}
	productos, err := replicarProductos(src, dst, cats)
	if err != nil {
		return err
	}
	if err := replicarStock(src, dst, productos, almacenes); err != nil {
		return err
	}
	if err := replicarClientes(src, dst); err != nil {
		return err
	}

	fmt.Printf("\n✓ Réplica terminada. Ahora apunta Integra al destino:\n")
	fmt.Printf("    ODOO_URL=%s\n    ODOO_DB=%s\n", destino.URL, destino.Database)
	fmt.Printf("    go run ./cmd/integra conectar-odoo && go run ./cmd/integra sync --completo\n")
	return nil
}

// replicarCompania alinea nombre y moneda con los del origen.
//
// Es lo primero que hay que arreglar y lo más fácil de pasar por alto: una
// base nueva de Odoo nace como «My Company» en dólares, así que TODO pedido
// que Integra cree se valora en USD. Los totales parecen correctos —el número
// es el mismo— y están en la moneda equivocada.
func replicarCompania(src, dst *odoo.Client) error {
	origen, err := src.SearchRead("res.company", nil,
		[]string{"name", "currency_id", "country_id", "vat"},
		map[string]interface{}{"limit": 1, "order": "id"})
	if err != nil {
		return fmt.Errorf("leyendo la compañía del origen: %w", err)
	}
	if len(origen) == 0 {
		return fmt.Errorf("el origen no tiene compañía")
	}
	o := origen[0]
	moneda := o.RefName("currency_id")
	fmt.Printf("Compañía: %s (%s)\n", o.Str("name"), moneda)

	destino, err := dst.SearchRead("res.company", nil,
		[]string{"name", "currency_id"}, map[string]interface{}{"limit": 1, "order": "id"})
	if err != nil || len(destino) == 0 {
		return fmt.Errorf("leyendo la compañía del destino: %w", err)
	}
	d := destino[0]
	if d.RefName("currency_id") == moneda && d.Str("name") == o.Str("name") {
		fmt.Printf("  → ya coincide\n\n")
		return nil
	}

	// La moneda tiene que estar activa antes de poder asignarla: Odoo trae
	// todas las del mundo pero solo unas pocas habilitadas.
	monedas, err := dst.SearchRead("res.currency",
		[]interface{}{[]interface{}{"name", "=", moneda}},
		[]string{"active"},
		map[string]interface{}{"limit": 1, "context": map[string]interface{}{"active_test": false}})
	if err != nil {
		return err
	}
	if len(monedas) == 0 {
		return fmt.Errorf("la moneda %s no existe en el destino", moneda)
	}
	monedaID := monedas[0].ID()
	if !monedas[0].Bool("active") {
		if err := dst.Write("res.currency", []int64{monedaID},
			map[string]interface{}{"active": true}); err != nil {
			return fmt.Errorf("activando %s: %w", moneda, err)
		}
	}

	valores := map[string]interface{}{
		"name":        o.Str("name"),
		"currency_id": monedaID,
	}
	if v := o.Str("vat"); v != "" {
		valores["vat"] = v
	}
	if err := dst.Write("res.company", []int64{d.ID()}, valores); err != nil {
		return fmt.Errorf("actualizando la compañía: %w", err)
	}
	fmt.Printf("  → «%s» pasó de %s a %s\n\n", d.Str("name"), d.RefName("currency_id"), moneda)
	return nil
}

// replicarImpuestos copia los impuestos de venta.
//
// Sin ellos los pedidos se crean sin IVA y no se parecen a lo que verá
// contabilidad, que es justo lo que hay que validar con ellos antes de
// automatizar nada.
func replicarImpuestos(src, dst *odoo.Client) error {
	filas, err := src.SearchRead("account.tax",
		[]interface{}{[]interface{}{"type_tax_use", "=", "sale"}},
		[]string{"name", "amount", "amount_type", "price_include_override", "description"},
		map[string]interface{}{"limit": 100, "order": "id"})
	if err != nil {
		// Sin módulo de contabilidad en el destino esto no es un fallo grave:
		// se avisa y se sigue con el resto de la réplica.
		fmt.Printf("Impuestos: no se pudieron leer (%v)\n\n", err)
		return nil
	}
	fmt.Printf("Impuestos de venta: %d en el origen\n", len(filas))

	existentes := map[string]bool{}
	dstFilas, err := dst.SearchRead("account.tax",
		[]interface{}{[]interface{}{"type_tax_use", "=", "sale"}},
		[]string{"name"}, map[string]interface{}{"limit": 200})
	if err != nil {
		fmt.Printf("  → el destino no tiene contabilidad instalada: se omiten\n\n")
		return nil
	}
	for _, f := range dstFilas {
		existentes[strings.ToLower(f.Str("name"))] = true
	}

	nuevos := 0
	for _, f := range filas {
		if existentes[strings.ToLower(f.Str("name"))] {
			continue
		}
		tipo := f.Str("amount_type")
		if tipo == "" {
			tipo = "percent"
		}
		if _, err := dst.Create("account.tax", map[string]interface{}{
			"name":         f.Str("name"),
			"amount":       f.Float("amount"),
			"amount_type":  tipo,
			"type_tax_use": "sale",
		}); err != nil {
			fmt.Printf("  ✗ %s: %v\n", f.Str("name"), err)
			continue
		}
		nuevos++
	}
	fmt.Printf("  → %d creados, %d ya existían\n\n", nuevos, len(filas)-nuevos)
	return nil
}

// replicarTarifas alinea las listas de precios y, sobre todo, su moneda.
//
// La moneda del pedido de venta NO sale de la compañía sino de la tarifa
// (sale.order.currency_id viene de pricelist_id.currency_id). Cambiar la
// compañía a COP y dejar la tarifa en USD produce pedidos que se ven bien y
// están valorados en dólares — el error más silencioso de todos.
func replicarTarifas(src, dst *odoo.Client) error {
	filas, err := src.SearchRead("product.pricelist", nil,
		[]string{"name", "currency_id"}, map[string]interface{}{"limit": 100, "order": "id"})
	if err != nil {
		fmt.Printf("Tarifas: no se pudieron leer (%v)\n\n", err)
		return nil
	}
	fmt.Printf("Tarifas: %d en el origen\n", len(filas))
	if len(filas) == 0 {
		fmt.Println()
		return nil
	}

	// La moneda de la primera tarifa del origen es la que manda: es la que
	// usa Odoo por defecto al crear pedidos.
	moneda := filas[0].RefName("currency_id")
	monedaID, err := idDeMoneda(dst, moneda)
	if err != nil {
		return err
	}

	dstFilas, err := dst.SearchRead("product.pricelist", nil,
		[]string{"name", "currency_id"}, map[string]interface{}{"limit": 200})
	if err != nil {
		return err
	}
	existentes := map[string]bool{}
	corregidas := 0
	for _, f := range dstFilas {
		existentes[strings.ToLower(f.Str("name"))] = true
		// Toda tarifa del destino tiene que quedar en la moneda correcta,
		// incluida la «Predeterminado» que Odoo crea sola al nacer la base.
		if f.RefName("currency_id") != moneda {
			if err := dst.Write("product.pricelist", []int64{f.ID()},
				map[string]interface{}{"currency_id": monedaID}); err != nil {
				fmt.Printf("  ✗ %s: %v\n", f.Str("name"), err)
				continue
			}
			corregidas++
		}
	}

	nuevas := 0
	for _, f := range filas {
		if existentes[strings.ToLower(f.Str("name"))] {
			continue
		}
		mID := monedaID
		if m := f.RefName("currency_id"); m != moneda {
			if otro, err := idDeMoneda(dst, m); err == nil {
				mID = otro
			}
		}
		if _, err := dst.Create("product.pricelist", map[string]interface{}{
			"name": f.Str("name"), "currency_id": mID,
		}); err != nil {
			fmt.Printf("  ✗ %s: %v\n", f.Str("name"), err)
			continue
		}
		nuevas++
	}
	fmt.Printf("  → %d creadas, %d corregidas a %s\n\n", nuevas, corregidas, moneda)
	return nil
}

// idDeMoneda busca una moneda en el destino y la activa si hace falta.
func idDeMoneda(dst *odoo.Client, nombre string) (int64, error) {
	filas, err := dst.SearchRead("res.currency",
		[]interface{}{[]interface{}{"name", "=", nombre}},
		[]string{"active"},
		map[string]interface{}{"limit": 1, "context": map[string]interface{}{"active_test": false}})
	if err != nil {
		return 0, err
	}
	if len(filas) == 0 {
		return 0, fmt.Errorf("la moneda %s no existe en el destino", nombre)
	}
	if !filas[0].Bool("active") {
		if err := dst.Write("res.currency", []int64{filas[0].ID()},
			map[string]interface{}{"active": true}); err != nil {
			return 0, fmt.Errorf("activando %s: %w", nombre, err)
		}
	}
	return filas[0].ID(), nil
}

// replicarClientes copia los clientes para poder probar el emparejamiento.
//
// Sin ellos, cada pedido que llega de un canal crea un cliente nuevo y nunca
// se ejercita la búsqueda por correo, que es donde aparecerían los duplicados.
func replicarClientes(src, dst *odoo.Client) error {
	filas, err := src.SearchRead("res.partner",
		[]interface{}{
			[]interface{}{"customer_rank", ">", 0},
			[]interface{}{"active", "=", true},
		},
		[]string{"name", "email", "phone", "vat", "street", "city", "is_company"},
		map[string]interface{}{"limit": 2000, "order": "id"})
	if err != nil {
		return fmt.Errorf("leyendo clientes del origen: %w", err)
	}
	fmt.Printf("Clientes: %d en el origen\n", len(filas))
	if len(filas) == 0 {
		fmt.Println()
		return nil
	}

	// Se emparejan por correo, que es lo que usa la ingesta de pedidos; los
	// que no tienen correo, por nombre exacto.
	porCorreo := map[string]bool{}
	porNombre := map[string]bool{}
	dstFilas, err := dst.SearchRead("res.partner", nil,
		[]string{"name", "email"}, map[string]interface{}{"limit": 5000})
	if err != nil {
		return err
	}
	for _, f := range dstFilas {
		if e := strings.ToLower(strings.TrimSpace(f.Str("email"))); e != "" {
			porCorreo[e] = true
		}
		porNombre[strings.ToLower(strings.TrimSpace(f.Str("name")))] = true
	}

	nuevos, omitidos := 0, 0
	for _, f := range filas {
		correo := strings.ToLower(strings.TrimSpace(f.Str("email")))
		nombre := strings.TrimSpace(f.Str("name"))
		if nombre == "" {
			omitidos++
			continue
		}
		if (correo != "" && porCorreo[correo]) || (correo == "" && porNombre[strings.ToLower(nombre)]) {
			omitidos++
			continue
		}

		valores := map[string]interface{}{
			"name": nombre, "customer_rank": 1, "is_company": f.Bool("is_company"),
		}
		for campo, valor := range map[string]string{
			"email": f.Str("email"), "phone": f.Str("phone"),
			"vat": f.Str("vat"), "street": f.Str("street"), "city": f.Str("city"),
		} {
			if v := strings.TrimSpace(valor); v != "" {
				valores[campo] = v
			}
		}
		if _, err := dst.Create("res.partner", valores); err != nil {
			omitidos++
			continue
		}
		if correo != "" {
			porCorreo[correo] = true
		}
		porNombre[strings.ToLower(nombre)] = true
		nuevos++
	}
	fmt.Printf("  → %d creados, %d ya existían o sin nombre\n", nuevos, omitidos)
	return nil
}

// replicarCategorias copia el árbol respetando la jerarquía.
//
// Se recorre por profundidad: una categoría no se puede crear antes que su
// padre, y el orden en que las devuelve Odoo no lo garantiza.
func replicarCategorias(src, dst *odoo.Client) (map[int64]int64, error) {
	filas, err := src.SearchRead("product.category", nil,
		[]string{"name", "parent_id", "complete_name"},
		map[string]interface{}{"limit": 5000, "order": "parent_path"})
	if err != nil {
		return nil, fmt.Errorf("leyendo categorías del origen: %w", err)
	}
	fmt.Printf("Categorías: %d en el origen\n", len(filas))

	// Mapa de la ruta completa al id del destino: la ruta es lo único
	// estable entre instancias, porque los identificadores no coinciden.
	rutaDestino := map[string]int64{}
	dstFilas, err := dst.SearchRead("product.category", nil,
		[]string{"complete_name"}, map[string]interface{}{"limit": 5000})
	if err != nil {
		return nil, err
	}
	for _, f := range dstFilas {
		rutaDestino[f.Str("complete_name")] = f.ID()
	}

	mapa := map[int64]int64{}
	porID := map[int64]odoo.Record{}
	for _, f := range filas {
		porID[f.ID()] = f
	}

	// crear devuelve si REALMENTE creó la categoría. Contar por el tamaño del
	// mapa no sirve: también crece al emparejar una que ya existía, y eso
	// hacía informar de 268 categorías nuevas donde no se creó ninguna.
	nuevas := 0
	var crear func(f odoo.Record) (int64, error)
	crear = func(f odoo.Record) (int64, error) {
		if id, ok := mapa[f.ID()]; ok {
			return id, nil
		}
		ruta := f.Str("complete_name")
		if id, ok := rutaDestino[ruta]; ok {
			mapa[f.ID()] = id
			return id, nil
		}

		valores := map[string]interface{}{"name": f.Str("name")}
		if padre := f.RefID("parent_id"); padre != 0 {
			pf, ok := porID[padre]
			if ok {
				pid, err := crear(pf)
				if err != nil {
					return 0, err
				}
				valores["parent_id"] = pid
			}
		}
		id, err := dst.Create("product.category", valores)
		if err != nil {
			return 0, fmt.Errorf("creando categoría %q: %w", ruta, err)
		}
		mapa[f.ID()] = id
		rutaDestino[ruta] = id
		nuevas++
		return id, nil
	}

	for _, f := range filas {
		if _, err := crear(f); err != nil {
			return nil, err
		}
	}
	fmt.Printf("  → %d creadas, %d ya existían\n\n", nuevas, len(filas)-nuevas)
	return mapa, nil
}

func replicarAlmacenes(src, dst *odoo.Client) (map[int64]int64, error) {
	filas, err := src.SearchRead("stock.warehouse", nil,
		[]string{"name", "code"}, map[string]interface{}{"order": "id"})
	if err != nil {
		return nil, fmt.Errorf("leyendo almacenes del origen: %w", err)
	}
	fmt.Printf("Almacenes: %d en el origen\n", len(filas))

	porCodigo := map[string]int64{}
	dstFilas, err := dst.SearchRead("stock.warehouse", nil,
		[]string{"code"}, map[string]interface{}{"limit": 200})
	if err != nil {
		return nil, err
	}
	for _, f := range dstFilas {
		porCodigo[strings.ToUpper(f.Str("code"))] = f.ID()
	}

	mapa := map[int64]int64{}
	nuevos := 0
	for _, f := range filas {
		codigo := strings.ToUpper(f.Str("code"))
		if id, ok := porCodigo[codigo]; ok {
			mapa[f.ID()] = id
			continue
		}
		id, err := dst.Create("stock.warehouse", map[string]interface{}{
			"name": f.Str("name"), "code": f.Str("code"),
		})
		if err != nil {
			// Un almacén que no se puede crear no debe abortar la réplica:
			// su stock simplemente no viajará.
			fmt.Printf("  ✗ %s: %v\n", f.Str("name"), err)
			continue
		}
		mapa[f.ID()] = id
		porCodigo[codigo] = id
		nuevos++
	}
	fmt.Printf("  → %d creados, %d ya existían\n\n", nuevos, len(mapa)-nuevos)
	return mapa, nil
}

// replicarProductos copia el catálogo emparejando por referencia interna.
func replicarProductos(src, dst *odoo.Client, cats map[int64]int64) (map[int64]int64, error) {
	// Se leen también los archivados: el catálogo real los tiene y conviene
	// que el entorno de pruebas se le parezca.
	filas, err := src.SearchRead("product.product", nil,
		[]string{"default_code", "barcode", "name", "description_sale", "categ_id",
			"list_price", "standard_price", "weight", "volume", "sale_ok",
			"purchase_ok", "is_storable", "active", "type"},
		map[string]interface{}{
			"limit": 5000, "order": "id",
			"context": map[string]interface{}{"active_test": false},
		})
	if err != nil {
		return nil, fmt.Errorf("leyendo productos del origen: %w", err)
	}
	fmt.Printf("Productos: %d en el origen\n", len(filas))

	porSKU := map[string]int64{}
	dstFilas, err := dst.SearchRead("product.product", nil,
		[]string{"default_code"}, map[string]interface{}{
			"limit": 10000, "context": map[string]interface{}{"active_test": false},
		})
	if err != nil {
		return nil, err
	}
	for _, f := range dstFilas {
		if sku := strings.TrimSpace(f.Str("default_code")); sku != "" {
			porSKU[strings.ToUpper(sku)] = f.ID()
		}
	}

	mapa := map[int64]int64{}
	nuevos, actualizados, omitidos := 0, 0, 0

	for i, f := range filas {
		sku := strings.TrimSpace(f.Str("default_code"))
		if sku == "" {
			// Sin referencia no hay forma de emparejar en una segunda pasada,
			// y duplicaríamos el producto en cada ejecución.
			omitidos++
			continue
		}

		valores := map[string]interface{}{
			"name":           f.Str("name"),
			"default_code":   sku,
			"list_price":     f.Float("list_price"),
			"standard_price": f.Float("standard_price"),
			"weight":         f.Float("weight"),
			"volume":         f.Float("volume"),
			"sale_ok":        f.Bool("sale_ok"),
			"purchase_ok":    f.Bool("purchase_ok"),
			"active":         f.Bool("active"),
			// En Odoo 18 «almacenable» son DOS cosas: type = "consu" y
			// is_storable = true. Copiar solo el tipo deja un consumible sin
			// inventario, y entonces Odoo rechaza crear existencias con
			// «Quants cannot be created for consumables or services».
			"type":        tipoDe(f),
			"is_storable": f.Bool("is_storable"),
		}
		if b := strings.TrimSpace(f.Str("barcode")); b != "" {
			valores["barcode"] = b
		}
		if d := f.Str("description_sale"); d != "" {
			valores["description_sale"] = d
		}
		if cid := cats[f.RefID("categ_id")]; cid != 0 {
			valores["categ_id"] = cid
		}

		if id, ok := porSKU[strings.ToUpper(sku)]; ok {
			// El producto ya está: se actualiza por si cambió en el origen.
			if err := dst.Write("product.product", []int64{id}, valores); err != nil {
				fmt.Printf("  ✗ %s: %v\n", sku, err)
				continue
			}
			mapa[f.ID()] = id
			actualizados++
		} else {
			id, err := dst.Create("product.product", valores)
			if err != nil {
				fmt.Printf("  ✗ %s: %v\n", sku, err)
				continue
			}
			mapa[f.ID()] = id
			porSKU[strings.ToUpper(sku)] = id
			nuevos++
		}

		if (i+1)%50 == 0 {
			fmt.Printf("  … %d/%d\n", i+1, len(filas))
		}
	}
	fmt.Printf("  → %d creados, %d actualizados, %d omitidos sin referencia\n\n",
		nuevos, actualizados, omitidos)
	return mapa, nil
}

// tipoDe traduce el tipo de producto.
//
// Odoo 18 dejó "product" atrás: ahora todo lo que no es servicio ni combo es
// "consu", y lo que decide si lleva inventario es is_storable.
func tipoDe(f odoo.Record) string {
	switch f.Str("type") {
	case "service":
		return "service"
	case "combo":
		return "combo"
	default:
		return "consu"
	}
}

// replicarStock ajusta las existencias creando movimientos de inventario.
//
// No se escribe stock.quant directamente: Odoo lo permite pero deja el
// inventario sin trazabilidad contable. Con inventory_quantity y su método de
// aplicación, el ajuste queda registrado como en una toma de inventario real.
// modoInventario es el contexto sin el cual stock.quant ignora los ajustes.
//
// Odoo comprueba _is_inventory_mode() antes de dejar tocar las cantidades: sin
// esta bandera la escritura se acepta y no hace nada.
var modoInventario = map[string]interface{}{"inventory_mode": true}

func replicarStock(src, dst *odoo.Client, productos, almacenes map[int64]int64) error {
	if len(productos) == 0 {
		fmt.Println("Existencias: nada que copiar (no se replicó ningún producto)")
		return nil
	}

	// Ubicación de stock de cada almacén del destino.
	ubicacionDe := map[int64]int64{}
	for _, dstID := range almacenes {
		filas, err := dst.SearchRead("stock.warehouse",
			[]interface{}{[]interface{}{"id", "=", dstID}},
			[]string{"lot_stock_id"}, map[string]interface{}{"limit": 1})
		if err != nil || len(filas) == 0 {
			continue
		}
		ubicacionDe[dstID] = filas[0].RefID("lot_stock_id")
	}

	// Ubicación interna → almacén, en el origen.
	locs, err := src.SearchRead("stock.location",
		[]interface{}{[]interface{}{"usage", "=", "internal"}},
		[]string{"warehouse_id"},
		map[string]interface{}{"limit": 5000, "context": map[string]interface{}{"active_test": false}})
	if err != nil {
		return fmt.Errorf("leyendo ubicaciones del origen: %w", err)
	}
	almacenDeUbicacion := map[int64]int64{}
	for _, l := range locs {
		if w := l.RefID("warehouse_id"); w != 0 {
			almacenDeUbicacion[l.ID()] = w
		}
	}

	quants, err := src.SearchRead("stock.quant",
		[]interface{}{[]interface{}{"location_id.usage", "=", "internal"}},
		[]string{"product_id", "location_id", "quantity"},
		map[string]interface{}{"limit": 20000})
	if err != nil {
		return fmt.Errorf("leyendo existencias del origen: %w", err)
	}
	fmt.Printf("Existencias: %d registros en el origen\n", len(quants))

	// Los motivos se cuentan por separado: «19 saltados» sin decir por qué
	// obliga a adivinar, y ya pasó una vez.
	aplicados := 0
	motivos := map[string]int{}
	var primerError error

	for _, q := range quants {
		cantidad := q.Float("quantity")
		if cantidad == 0 {
			continue
		}
		prodDst, ok := productos[q.RefID("product_id")]
		if !ok {
			motivos["el producto no se replicó (sin referencia interna)"]++
			continue
		}
		almOrigen, ok := almacenDeUbicacion[q.RefID("location_id")]
		if !ok {
			motivos["la ubicación no pertenece a ningún almacén"]++
			continue
		}
		ubic, ok := ubicacionDe[almacenes[almOrigen]]
		if !ok || ubic == 0 {
			motivos["el almacén de destino no tiene ubicación de stock"]++
			continue
		}

		// El ajuste va en DOS pasos, y el orden importa:
		//
		//   1. localizar o crear el quant (producto + ubicación)
		//   2. ESCRIBIR inventory_quantity_auto_apply sobre él
		//
		// Ponerlo en el create no funciona: es un campo calculado con
		// inverso, y el inverso solo se dispara en write. Con el create la
		// llamada no da error y el quant queda en cero — un falso positivo
		// que ya costó dar por buena una réplica vacía.
		//
		// La otra vía —inventory_quantity + action_apply_inventory— tampoco
		// sirve por XML-RPC: ese método devuelve None y Odoo responde
		// «cannot marshal None unless allow_none is enabled».
		quantID, err := dst.BuscarUno("stock.quant", []interface{}{
			[]interface{}{"product_id", "=", prodDst},
			[]interface{}{"location_id", "=", ubic},
		})
		if err != nil {
			motivos["no se pudo buscar el quant"]++
			if primerError == nil {
				primerError = err
			}
			continue
		}
		if quantID == 0 {
			quantID, err = dst.CreateCon("stock.quant", map[string]interface{}{
				"product_id": prodDst, "location_id": ubic,
			}, modoInventario)
			if err != nil {
				motivos["no se pudo crear el quant"]++
				if primerError == nil {
					primerError = err
				}
				continue
			}
		}
		if err := dst.WriteCon("stock.quant", []int64{quantID}, map[string]interface{}{
			"inventory_quantity_auto_apply": cantidad,
		}, modoInventario); err != nil {
			motivos["no se pudo aplicar la cantidad"]++
			if primerError == nil {
				primerError = err
			}
			continue
		}
		aplicados++
	}

	fmt.Printf("  → %d ajustes aplicados\n", aplicados)
	for motivo, n := range motivos {
		fmt.Printf("     %d saltados: %s\n", n, motivo)
	}
	if primerError != nil {
		fmt.Printf("\n  Primer error: %v\n", primerError)
	}

	// Se comprueba el RESULTADO, no la ausencia de error. Una llamada que no
	// falla puede dejar el quant en cero, y contar eso como éxito da una
	// réplica vacía que parece correcta.
	total, err := sumaExistencias(dst)
	if err != nil {
		return err
	}
	fmt.Printf("  → %.0f unidades en el destino\n", total)
	if aplicados > 0 && total == 0 {
		return fmt.Errorf("se aplicaron %d ajustes pero el destino sigue con cero unidades: "+
			"la cantidad no llegó a escribirse", aplicados)
	}
	return nil
}

// sumaExistencias lee del destino lo que de verdad quedó guardado.
func sumaExistencias(dst *odoo.Client) (float64, error) {
	filas, err := dst.ReadGroup("stock.quant",
		[]interface{}{[]interface{}{"location_id.usage", "=", "internal"}},
		[]string{"quantity"}, nil, nil)
	if err != nil {
		return 0, fmt.Errorf("comprobando las existencias del destino: %w", err)
	}
	var total float64
	for _, f := range filas {
		total += f.Float("quantity")
	}
	return total, nil
}

func env(clave, porDefecto string) string {
	if v := strings.TrimSpace(os.Getenv(clave)); v != "" {
		return v
	}
	return porDefecto
}
