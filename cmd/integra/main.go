// Command integra es el binario único de la plataforma.
//
// API y worker son el mismo ejecutable con distinto subcomando: así no pueden
// divergir ni las dependencias ni la versión del esquema entre los procesos.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	// Windows no trae base de datos de zonas horarias, así que time.LoadLocation
	// depende de que exista zoneinfo.zip dentro de la instalación de Go. Eso
	// convierte a «America/Bogota» en una zona inválida en cualquier máquina
	// que no tenga Go, y también en una que lo tenga si se limpia la caché de
	// módulos. Importar tzdata la embebe en el ejecutable —unos 450 KB— y el
	// binario deja de depender de nada externo.
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"golang.org/x/term"

	"github.com/mdv/integra/internal/api"
	"github.com/mdv/integra/internal/atributos"
	"github.com/mdv/integra/internal/auth"
	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/conectores/falabella"
	"github.com/mdv/integra/internal/config"
	"github.com/mdv/integra/internal/content"
	"github.com/mdv/integra/internal/crypto"
	"github.com/mdv/integra/internal/ia"
	"github.com/mdv/integra/internal/imagen"
	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/mercadolibre"
	"github.com/mdv/integra/internal/migrate"
	"github.com/mdv/integra/internal/notificaciones"
	"github.com/mdv/integra/internal/odoo"
	"github.com/mdv/integra/internal/ordenes"
	"github.com/mdv/integra/internal/planificador"
	"github.com/mdv/integra/internal/publicar"
	"github.com/mdv/integra/internal/store"
	"github.com/mdv/integra/internal/sync"
	"github.com/mdv/integra/migrations"
)

func main() {
	if len(os.Args) < 2 {
		uso()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "genkey":
		err = cmdGenkey()
	case "migrate":
		err = cmdMigrate(ctx, os.Args[2:])
	case "conectar-odoo":
		err = cmdConectarOdoo(ctx)
	case "sync":
		err = cmdSync(ctx, os.Args[2:])
	case "generar-contenido":
		err = cmdGenerarContenido(ctx, os.Args[2:])
	case "sugerir-categorias":
		err = cmdSugerirCategorias(ctx)
	case "reprocesar-imagenes":
		err = cmdReprocesarImagenes(ctx)
	case "verificar-imagenes":
		err = cmdVerificarImagenes(ctx, os.Args[2:])
	case "atributos":
		err = cmdAtributos(ctx)
	case "ordenes":
		err = cmdOrdenes(ctx)
	case "conexiones":
		err = cmdConexiones(ctx, os.Args[2:])
	case "crear-usuario":
		err = cmdCrearUsuario(ctx, os.Args[2:])
	case "usuarios":
		err = cmdListarUsuarios(ctx)
	case "desactivar-usuario":
		err = cmdCambiarUsuario(ctx, "desactivar", os.Args[2:])
	case "activar-usuario":
		err = cmdCambiarUsuario(ctx, "activar", os.Args[2:])
	case "borrar-usuario":
		err = cmdCambiarUsuario(ctx, "borrar", os.Args[2:])
	case "cambiar-password":
		err = cmdCambiarPassword(ctx, os.Args[2:])
	case "ia-probar":
		err = cmdIAProbar(ctx, os.Args[2:])
	case "serve":
		err = cmdServe(ctx)
	case "worker":
		err = cmdWorker(ctx)
	case "help", "-h", "--help":
		uso()
		return
	default:
		fmt.Fprintf(os.Stderr, "subcomando desconocido: %s\n\n", os.Args[1])
		uso()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "\n✗ %v\n", err)
		os.Exit(1)
	}
}

func uso() {
	fmt.Fprint(os.Stderr, `integra — sincronización de catálogo entre Odoo y los canales de venta

Uso:
  integra genkey            genera una clave maestra de cifrado
  integra migrate up        aplica las migraciones pendientes
  integra migrate down      revierte la última migración
  integra migrate status    muestra qué está aplicado
  integra conectar-odoo     guarda la conexión a Odoo (cifrada) desde ODOO_*
  integra sync              trae el catálogo de Odoo
  integra generar-contenido genera títulos y descripciones
  integra sugerir-categorias sugiere la categoría de MercadoLibre de cada rama
  integra reprocesar-imagenes transcodifica a JPEG los WebP del banco
  integra generar-contenido --ia redacta las fichas con el modelo configurado
  integra verificar-imagenes [--todas] [--limite N]
                            comprueba con IA que cada portada muestre su producto
  integra ia-probar         redacta una ficha de prueba y mide cuánto tarda
  integra atributos         trae los atributos que exigen los marketplaces
  integra ordenes           monta en Odoo los pedidos pendientes
  integra conexiones        lista las conexiones a Odoo y el trabajo de cada una
  integra conexiones migrar <origen> <destino>   traslada por SKU precios,
                            marcas, descripciones, imágenes y atributos
  integra conexiones borrar <id>                 elimina una conexión inactiva
  integra crear-usuario <email> <nombre> <password> [rol]  crea un usuario (admin|operator|viewer)
  integra usuarios          lista los usuarios registrados
  integra desactivar-usuario <email>   le quita el acceso sin borrar su rastro
  integra activar-usuario <email>      se lo devuelve
  integra borrar-usuario <email> --confirmar  lo elimina de la base
  integra cambiar-password <email> [clave]    le pone una contraseña nueva
  integra sync --completo   ignora la marca de agua y relee todo
  integra serve             arranca el servidor HTTP
  integra worker            arranca el procesador de trabajos

Variables de entorno:
  INTEGRA_DATABASE_URL   cadena de conexión a PostgreSQL (obligatoria)
  INTEGRA_MASTER_KEY     clave de cifrado en base64 (obligatoria)
  INTEGRA_HTTP_ADDR      dirección de escucha, por defecto :8080
  INTEGRA_LOG_LEVEL      debug | info | warn | error
  INTEGRA_TIMEZONE       zona horaria, por defecto America/Bogota

  ODOO_URL, ODOO_DB, ODOO_USER, ODOO_API_KEY   solo para conectar-odoo
`)
}

func registro(nivel string) *slog.Logger {
	l := slog.LevelInfo
	switch nivel {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

func cmdGenkey() error {
	clave, err := crypto.GenerarClave()
	if err != nil {
		return err
	}
	fmt.Printf("INTEGRA_MASTER_KEY=%s\n", clave)
	fmt.Fprint(os.Stderr, `
Guárdala en tu gestor de secretos ANTES de cifrar nada.

Si se pierde, las credenciales de Odoo y de los 16 canales quedan
irrecuperables y hay que volver a introducirlas todas a mano.
`)
	return nil
}

func cmdMigrate(ctx context.Context, args []string) error {
	accion := "up"
	if len(args) > 0 {
		accion = args[0]
	}

	dsn := os.Getenv("INTEGRA_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("INTEGRA_DATABASE_URL es obligatoria")
	}

	migs, err := migrate.Cargar(migrations.FS)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("conectando a PostgreSQL: %w", err)
	}
	defer conn.Close(context.Background())

	switch accion {
	case "up":
		hechas, err := migrate.Up(ctx, conn, migs)
		if err != nil {
			return err
		}
		if len(hechas) == 0 {
			fmt.Println("Sin migraciones pendientes.")
			return nil
		}
		for _, n := range hechas {
			fmt.Printf("  ✓ %s\n", n)
		}
		fmt.Printf("\n%d migraciones aplicadas.\n", len(hechas))

	case "down":
		nombre, err := migrate.Down(ctx, conn, migs)
		if err != nil {
			return err
		}
		if nombre == "" {
			fmt.Println("No hay nada que revertir.")
			return nil
		}
		fmt.Printf("  ✓ revertida %s\n", nombre)

	case "status":
		estados, err := migrate.Status(ctx, conn, migs)
		if err != nil {
			return err
		}
		pendientes := 0
		for _, e := range estados {
			marca := "pendiente"
			if e.Aplicada {
				marca = "aplicada "
			} else {
				pendientes++
			}
			fmt.Printf("  [%s] %s\n", marca, e.Nombre)
		}
		fmt.Printf("\n%d de %d aplicadas.\n", len(estados)-pendientes, len(estados))

	default:
		return fmt.Errorf("acción de migrate desconocida: %s (up, down o status)", accion)
	}
	return nil
}

// cmdConectarOdoo guarda la conexión con la clave cifrada.
//
// Se comprueba que la credencial funciona ANTES de guardarla: descubrir que
// era inválida durante la primera sincronización nocturna sería mucho peor.
func cmdConectarOdoo(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cif, err := crypto.DesdeBase64(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("INTEGRA_MASTER_KEY inválida: %w", err)
	}

	oc := odoo.Config{
		URL:      os.Getenv("ODOO_URL"),
		Database: os.Getenv("ODOO_DB"),
		Username: os.Getenv("ODOO_USER"),
		APIKey:   os.Getenv("ODOO_API_KEY"),
	}
	if oc.URL == "" || oc.Database == "" || oc.Username == "" || oc.APIKey == "" {
		return fmt.Errorf("hacen falta ODOO_URL, ODOO_DB, ODOO_USER y ODOO_API_KEY")
	}

	fmt.Printf("Comprobando las credenciales contra %s…\n", oc.URL)
	cli, err := odoo.Connect(oc)
	if err != nil {
		return fmt.Errorf("las credenciales no funcionan, no se guarda nada: %w", err)
	}
	fmt.Printf("  ✓ autenticado (uid=%d)\n", cli.UID())

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	enc, err := cif.CifrarTexto(oc.APIKey)
	if err != nil {
		return err
	}
	id, err := st.GuardarConexionOdoo(ctx, "Odoo MDV", oc.URL, oc.Database, oc.Username, enc, cfg.DefaultTimezone)
	if err != nil {
		return err
	}

	fmt.Printf("  ✓ conexión %d guardada con la clave cifrada (%s)\n", id, crypto.Redactar(oc.APIKey))
	fmt.Printf("\nYa puedes ejecutar: integra sync\n")
	return nil
}

func cmdSync(ctx context.Context, args []string) error {
	completo := false
	for _, a := range args {
		if a == "--completo" || a == "-completo" {
			completo = true
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := registro(cfg.LogLevel)

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	cli, conexionID, desde, err := abrirOdoo(ctx, st, cfg.MasterKey, completo)
	if err != nil {
		return err
	}

	if desde.IsZero() {
		fmt.Printf("Sincronización completa del catálogo…\n\n")
	} else {
		fmt.Printf("Sincronización incremental desde %s…\n\n", desde.Format("2006-01-02 15:04"))
	}

	res, err := sync.Nuevo(cli, st, log).Catalogo(ctx, conexionID, desde)
	if err != nil {
		return err
	}

	fmt.Printf("  Leídos de Odoo   %d  (solo SKU, nombre y stock)\n", res.Leidos)
	fmt.Printf("  Guardados        %d\n", res.Guardados)
	fmt.Printf("  Almacenes        %d\n", res.Almacenes)
	fmt.Printf("  Duración         %s\n", res.Duracion.Round(time.Millisecond))
	fmt.Printf("\nPrecio, marca, descripción e imágenes se administran en Integra: el sync no los toca.\n")
	return nil
}

// abrirOdoo descifra la credencial guardada y abre la sesión.
func abrirOdoo(ctx context.Context, st *store.Store, claveMaestra string, completo bool) (*odoo.Client, int64, time.Time, error) {
	cif, err := crypto.DesdeBase64(claveMaestra)
	if err != nil {
		return nil, 0, time.Time{}, fmt.Errorf("INTEGRA_MASTER_KEY inválida: %w", err)
	}
	conex, err := st.ConexionOdooActiva(ctx)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	clave, err := cif.DescifrarTexto(conex.APIKeyCifrada)
	if err != nil {
		return nil, 0, time.Time{}, fmt.Errorf("no se pudo descifrar la clave de Odoo "+
			"(¿cambió INTEGRA_MASTER_KEY?): %w", err)
	}
	cli, err := odoo.Connect(odoo.Config{
		URL: conex.BaseURL, Database: conex.Database,
		Username: conex.Username, APIKey: clave,
	})
	if err != nil {
		return nil, 0, time.Time{}, err
	}

	var desde time.Time
	if !completo && conex.Watermark != nil {
		desde = *conex.Watermark
	}
	return cli, conex.ID, desde, nil
}

// cmdGenerarContenido produce títulos y descripciones para el catálogo.
//
// No toca Odoo ni ningún canal: trabaja sobre lo ya sincronizado, así que se
// puede lanzar cuantas veces haga falta mientras se afinan las reglas.
func cmdGenerarContenido(ctx context.Context, args []string) error {
	rehacer, conIA := false, false
	for _, a := range args {
		switch a {
		case "--rehacer", "-rehacer":
			rehacer = true
		case "--ia", "-ia":
			conIA = true
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	fuentes, err := st.ProductosParaGenerar(ctx, !rehacer)
	if err != nil {
		return err
	}
	if len(fuentes) == 0 {
		fmt.Println("No hay productos pendientes de generar.")
		return nil
	}

	if conIA {
		return generarConIA(ctx, st, fuentes)
	}

	fmt.Printf("Generando contenido para %d productos…\n\n", len(fuentes))

	conteo := map[content.Confianza]int{}
	for _, f := range fuentes {
		b := content.Generar(content.Fuente{
			Nombre: f.Nombre, Marca: f.Marca, CategPath: f.CategPath,
			SKU: f.SKU, Peso: f.Peso,
		})
		if err := st.GuardarContenido(ctx, f.ProductID,
			b.Titulos, b.Descripcion, b.Specs, string(b.Confianza), b.Avisos); err != nil {
			return err
		}
		conteo[b.Confianza]++
	}

	res, err := st.ResumenContenido(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("  Confianza alta    %4d  (listos para revisión rápida)\n", res.Alta)
	fmt.Printf("  Confianza media   %4d  (revisar con atención)\n", res.Media)
	fmt.Printf("  Confianza baja    %4d  (el nombre en Odoo no da para más)\n", res.Baja)
	fmt.Printf("  ─────────────────────\n")
	fmt.Printf("  Con contenido     %4d\n", res.Total)
	fmt.Printf("  Editados a mano   %4d  (nunca se sobrescriben)\n", res.Editados)
	fmt.Printf("  Aprobados         %4d\n", res.Aprobados)
	return nil
}

// generarConIA redacta las fichas con el modelo configurado. Respeta las filas
// editadas a mano igual que el generador de reglas: GuardarContenido no las
// pisa.
// cmdIAProbar comprueba la instalación con un producto de mentira.
//
// Existe porque el lote real dura horas: antes de lanzarlo conviene saber en
// treinta segundos si el modelo responde, si sabe devolver el JSON y —lo que
// más sorprende— cuánto tarda por ficha en esta máquina, que es lo que decide
// si el lote entero cabe en una tarde o en una noche.
func cmdIAProbar(ctx context.Context, args []string) error {
	cli, err := ia.Nuevo(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Proveedor: %s\n\n", cli.Descripcion())

	muestras, err := muestrasParaProbar(ctx, args)
	if err != nil {
		return err
	}

	var total time.Duration
	hechas := 0
	for _, m := range muestras {
		fmt.Printf("── %s · %s\n", m.SKU, recorta(m.Nombre, 62))
		inicio := time.Now()
		f, err := cli.GenerarFicha(ctx, m.Nombre, m.Marca, m.CategPath, m.SKU, "")
		tardo := time.Since(inicio)
		if err != nil {
			fmt.Printf("   ✗ %v\n\n", err)
			continue
		}
		total += tardo
		hechas++

		for _, canal := range []string{"mercadolibre", "falabella", "woocommerce", "shopify"} {
			t := f.Titulos[canal]
			fmt.Printf("   %-13s (%3d) %s\n", canal, len([]rune(t)), recorta(t, 66))
		}
		fmt.Printf("   %-13s (%3d) %s\n", "descripción", len([]rune(f.Descripcion)),
			recorta(strings.ReplaceAll(f.Descripcion, "\n", " "), 66))
		fmt.Printf("   %-13s %d · %.1fs\n\n", "specs", len(f.Specs), tardo.Seconds())
	}

	if hechas == 0 {
		return fmt.Errorf("ninguna de las %d fichas de prueba salió bien", len(muestras))
	}
	media := total / time.Duration(hechas)
	fmt.Printf("%d/%d fichas, %.1fs de media.\n", hechas, len(muestras), media.Seconds())
	fmt.Printf("A este ritmo, 450 fichas tardarían unos %s.\n", duracionLegible(media*450))
	fmt.Println("\nLee las descripciones antes de lanzar el lote: lo que hay que")
	fmt.Println("vigilar en un modelo pequeño es que copie el ejemplo del prompt o")
	fmt.Println("que invente cifras. Si no convence, prueba uno mayor:")
	fmt.Println("    ollama pull qwen2.5:7b && IA_MODELO=qwen2.5:7b integra ia-probar")
	return nil
}

// muestrasParaProbar saca productos REALES y de categorías distintas.
//
// Probar siempre con el mismo disco duro no dice nada: un modelo pequeño puede
// estar copiando el ejemplo del prompt —que también es un disco duro— y la
// ficha saldría perfecta hasta que le toque unos audífonos. La variedad es
// justo lo que destapa ese fallo.
func muestrasParaProbar(ctx context.Context, args []string) ([]store.FuenteContenido, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	defer st.Close()

	if len(args) > 0 {
		m, err := st.FuenteContenidoPorSKU(ctx, args[0])
		if err != nil {
			return nil, err
		}
		return []store.FuenteContenido{*m}, nil
	}
	return st.MuestraVariada(ctx, 4)
}

func duracionLegible(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%.0f segundos", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%.0f minutos", d.Minutes())
	default:
		return fmt.Sprintf("%.1f horas", d.Hours())
	}
}

func generarConIA(ctx context.Context, st *store.Store, fuentes []store.FuenteContenido) error {
	// Se comprueba el proveedor ANTES del lote: con un modelo local, lo que
	// falla casi siempre es que el servicio no está arrancado o que falta
	// descargar el modelo, y eso hay que saberlo en el segundo uno y no en el
	// producto trescientos.
	cli, err := ia.Nuevo(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Redactando las fichas de %d productos con %s…\n\n", len(fuentes), cli.Descripcion())

	hechas, fallos := 0, 0
	for i, f := range fuentes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ficha, err := cli.GenerarFicha(ctx, f.Nombre, f.Marca, f.CategPath, f.SKU, "")
		if err != nil {
			fallos++
			fmt.Printf("  ✗ %-30s %v\n", recorta(f.SKU, 30), err)
			// Tres fallos seguidos al principio ya no son de configuración
			// —eso lo descartó Comprobar—, así que apuntan al modelo: uno
			// demasiado pequeño para devolver la ficha completa.
			if fallos >= 3 && hechas == 0 {
				return fmt.Errorf("las tres primeras fichas fallaron con %s; "+
					"prueba un modelo mayor en IA_MODELO", cli.Descripcion())
			}
			continue
		}
		var specs []map[string]string
		for _, sp := range ficha.Specs {
			specs = append(specs, map[string]string{"clave": sp.Clave, "valor": sp.Valor})
		}
		// Confianza alta: la redacción de la IA parte de los mismos datos pero
		// llega a una ficha completa; la revisión humana sigue en la interfaz.
		if err := st.GuardarContenido(ctx, f.ProductID,
			ficha.Titulos, ficha.Descripcion, specs, "alta", nil); err != nil {
			return err
		}
		hechas++
		if (i+1)%25 == 0 {
			fmt.Printf("  … %d/%d\n", i+1, len(fuentes))
		}
	}
	fmt.Printf("\n  Fichas redactadas  %d\n", hechas)
	if fallos > 0 {
		fmt.Printf("  Con fallo          %d  (se pueden reintentar relanzando el comando)\n", fallos)
	}
	return nil
}

// cmdVerificarImagenes pregunta a la IA, foto por foto, si la portada de cada
// producto muestra de verdad ese producto. Los "no corresponde" aparecen en
// la cola de atención para que una persona cambie la portada.
func cmdVerificarImagenes(ctx context.Context, args []string) error {
	todas := false
	// El límite existe para poder mirar cómo va antes de comprometerse a
	// varias horas de GPU, y para partir el lote en tandas: como cada
	// veredicto se guarda al vuelo, cortar y retomar no repite trabajo.
	limite := 0
	for i, a := range args {
		switch {
		case a == "--todas" || a == "-todas":
			todas = true
		case a == "--limite" || a == "-limite":
			if i+1 < len(args) {
				n, err := strconv.Atoi(args[i+1])
				if err != nil || n <= 0 {
					return fmt.Errorf("--limite necesita un número positivo, no %q", args[i+1])
				}
				limite = n
			}
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	alm, err := imagen.NuevoAlmacen(cfg.ImageDir)
	if err != nil {
		return err
	}

	pendientes, err := st.ImagenesParaVerificar(ctx, todas)
	if err != nil {
		return err
	}
	if len(pendientes) == 0 {
		fmt.Println("No hay imágenes pendientes de verificar.")
		return nil
	}
	total := len(pendientes)
	if limite > 0 && limite < total {
		pendientes = pendientes[:limite]
		fmt.Printf("Limitado a %d de %d pendientes.\n", limite, total)
	}

	cli, err := ia.Nuevo(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Verificando %d imágenes contra su producto con %s…\n\n",
		len(pendientes), cli.Descripcion())

	conteo := map[string]int{}
	fallos := 0
	for i, p := range pendientes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		datos, err := alm.Leer(p.Ruta)
		if err != nil {
			fallos++
			continue
		}
		v, err := cli.VerificarImagen(ctx, datos, p.Nombre, p.Marca, p.SKU)
		if err != nil {
			fallos++
			fmt.Printf("  ✗ %-30s %v\n", recorta(p.SKU, 30), err)
			if fallos >= 3 && i+1 == fallos {
				return fmt.Errorf("las tres primeras imágenes fallaron con %s; "+
					"revisa que el modelo de visión sea multimodal (IA_MODELO_VISION)", cli.Descripcion())
			}
			continue
		}
		if err := st.GuardarVerificacion(ctx, p.ProductoID, p.ImagenID, v.Resultado, v.Nota); err != nil {
			return err
		}
		conteo[v.Resultado]++
		if v.Resultado == "no_corresponde" {
			fmt.Printf("  ⚠ %-30s %s\n", recorta(p.SKU, 30), v.Nota)
		}
		if (i+1)%50 == 0 {
			fmt.Printf("  … %d/%d\n", i+1, len(pendientes))
		}
	}

	// Los veredictos alimentan la cola de atención (portada equivocada).
	if err := st.RecalcularAtencion(ctx); err != nil {
		return err
	}

	fmt.Printf("\n  Corresponden    %d\n", conteo["corresponde"])
	fmt.Printf("  Dudosas         %d\n", conteo["dudosa"])
	fmt.Printf("  No corresponden %d  (en la cola de atención)\n", conteo["no_corresponde"])
	if fallos > 0 {
		fmt.Printf("  Con fallo       %d\n", fallos)
	}
	return nil
}

// cmdSugerirCategorias propone la categoría de MercadoLibre de cada rama de Odoo.
//
// Usa el predictor público, así que no hace falta tener cuenta ni credenciales.
// Las sugerencias quedan pendientes de confirmación: publicar en una categoría
// equivocada arrastra historial y no se arregla borrando la publicación.
func cmdSugerirCategorias(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	cats, err := st.CategoriasParaMapear(ctx, "mercadolibre", true)
	if err != nil {
		return err
	}
	if len(cats) == 0 {
		fmt.Println("No hay categorías pendientes de mapear.")
		return nil
	}

	fmt.Printf("Consultando el predictor de MercadoLibre para %d categorías…\n\n", len(cats))
	p := mercadolibre.NuevoPredictor(mercadolibre.SitioColombia)

	var aciertos, dudosas, fallos int
	for _, c := range cats {
		sugs, err := p.Predecir(ctx, c.TituloEjemplo, 3)
		if err != nil {
			fallos++
			fmt.Printf("  ✗ %-52s %v\n", recorta(c.CategPath, 52), err)
			// Un fallo puntual no debe abortar el lote: se sigue con el resto.
			continue
		}

		mejor := sugs[0]
		conf := mercadolibre.ConfianzaCon(mejor, 0, c.CategPath, "")
		if err := st.GuardarSugerenciaCategoria(ctx, "mercadolibre", c.CategPath,
			mejor.CategoriaID, mejor.Categoria, "ml_predictor", conf, mejor.Atributos); err != nil {
			return err
		}
		marca := "  "
		if mercadolibre.RequiereRevision(conf) {
			marca = "⚠ "
			dudosas++
		} else {
			aciertos++
		}
		fmt.Printf("%s%-50s → %-11s %s\n",
			marca, recorta(c.CategPath, 50), mejor.CategoriaID, recorta(mejor.Categoria, 32))

		// Ritmo suave: es un servicio público y gratuito, no conviene abusar.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}

	fmt.Printf("\n  %d coherentes, %d dudosas (⚠), %d sin resultado.\n", aciertos, dudosas, fallos)
	fmt.Printf("  Ninguna se usará para publicar hasta que la confirmes en la interfaz.\n")
	return nil
}

func recorta(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n+1:])
}

// cmdReprocesarImagenes transcodifica a JPEG los WebP que ya estaban en el
// banco. Desde el arreglo en la ingesta ya no entran WebP nuevos; esto pone al
// día lo descargado antes, conservando asociaciones, orden y portadas.
func cmdReprocesarImagenes(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.ImageDir == "" {
		return fmt.Errorf("INTEGRA_IMAGE_DIR está vacío: no hay banco de imágenes")
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	alm, err := imagen.NuevoAlmacen(cfg.ImageDir)
	if err != nil {
		return err
	}

	lista, err := st.ImagenesWebP(ctx)
	if err != nil {
		return err
	}
	if len(lista) == 0 {
		fmt.Println("No hay imágenes WebP: el banco ya está entero en JPEG/PNG.")
		return nil
	}
	fmt.Printf("Transcodificando %d imágenes WebP a JPEG…\n\n", len(lista))

	hechas, fallos := 0, 0
	for i, img := range lista {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		datos, err := alm.Leer(img.Ruta)
		if err != nil {
			fallos++
			fmt.Printf("  ✗ imagen %d: no se pudo leer (%v)\n", img.ID, err)
			continue
		}
		// Ingerir ya transcodifica el WebP a JPEG y regenera las derivadas.
		res, err := alm.Ingerir(datos, imagen.VariantesPorDefecto)
		if err != nil {
			fallos++
			fmt.Printf("  ✗ imagen %d: %v\n", img.ID, err)
			continue
		}
		derivadas := make([]struct {
			Variante, Ruta, Formato string
			Ancho, Alto, Bytes      int
		}, 0, len(res.Derivadas))
		for _, d := range res.Derivadas {
			derivadas = append(derivadas, struct {
				Variante, Ruta, Formato string
				Ancho, Alto, Bytes      int
			}{d.Variante, d.Ruta, d.Info.Formato, d.Info.Ancho, d.Info.Alto, d.Info.Bytes})
		}
		nuevaID, err := st.RegistrarImagen(ctx,
			res.Original.SHA256, res.RutaOrig, res.Original.Formato,
			res.Original.Ancho, res.Original.Alto, res.Original.Bytes,
			img.Origen, img.OrigenRef, derivadas)
		if err != nil {
			return err
		}
		huerfanas, err := st.SustituirImagen(ctx, img.ID, nuevaID)
		if err != nil {
			return err
		}
		for _, r := range huerfanas {
			_ = alm.Borrar(r)
		}
		hechas++
		if (i+1)%100 == 0 {
			fmt.Printf("  … %d/%d\n", i+1, len(lista))
		}
	}

	fmt.Printf("\n  Transcodificadas  %d\n", hechas)
	if fallos > 0 {
		fmt.Printf("  Con fallo         %d  (siguen en WebP; revisa los mensajes)\n", fallos)
	}
	return nil
}

// cmdAtributos trae de MercadoLibre los atributos que exige cada categoría
// mapeada y deduce el valor de cada producto con lo que Integra ya sabe.
//
// Es el paso que desbloquea publicar en MercadoLibre: sin los atributos
// obligatorios de la categoría, rechaza la publicación.
func cmdAtributos(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := registro(cfg.LogLevel)
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	// Cada canal que impone atributos aporta su propia fuente. Shopify y
	// WooCommerce no aparecen porque son tiendas propias: no exigen ninguno.
	fuentes := map[string]atributos.FuenteRequisitos{
		"mercadolibre": atributos.NuevaFuenteML(),
	}
	// Falabella sí necesita credenciales, así que solo entra si la cuenta
	// está conectada.
	if ad, err := adaptadorFalabella(ctx, st, cfg.MasterKey); err == nil {
		fuentes["falabella"] = atributos.NuevaFuenteFalabella(ad)
	} else {
		fmt.Printf("Falabella se omite: %v\n\n", err)
	}

	deductor := atributos.NuevoDeductor(st, log)
	for canal, fuente := range fuentes {
		fmt.Printf("── %s ──\n", canal)

		ref, err := atributos.Refrescar(ctx, st, canal, fuente)
		if err != nil {
			fmt.Printf("  ✗ %v\n\n", err)
			continue
		}
		fmt.Printf("  %d categorías · %d atributos · %d obligatorios",
			ref.Categorias, ref.Atributos, ref.Obligatorios)
		if ref.Fallos > 0 {
			fmt.Printf(" · %d categorías fallaron", ref.Fallos)
		}
		fmt.Println()

		res, err := deductor.Deducir(ctx, canal)
		if err != nil {
			return err
		}
		fmt.Printf("  %d productos · %d valores asignados · %d completos · %d obligatorios sin valor\n\n",
			res.Productos, res.Asignados, res.Completos, res.Faltantes)
	}
	return nil
}

// cmdOrdenes monta en Odoo los pedidos que llegaron de los canales.
//
// El worker ya lo hace por su cuenta; este comando existe para reintentar a
// mano lo que falló y para poder verificar el ciclo sin esperar al horario.
func cmdOrdenes(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := registro(cfg.LogLevel)
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	cif, err := crypto.DesdeBase64(cfg.MasterKey)
	if err != nil {
		return err
	}
	svc := ordenes.NuevoServicio(st, cif, log, func(c context.Context) (*odoo.Client, error) {
		cli, _, _, err := abrirOdoo(c, st, cfg.MasterKey, false)
		return cli, err
	})

	pendientes, err := st.OrdenesPendientesOdoo(ctx, 100)
	if err != nil {
		return err
	}
	if len(pendientes) == 0 {
		fmt.Println("No hay pedidos pendientes de montar en Odoo.")
		return nil
	}
	fmt.Printf("Montando %d pedidos en Odoo…\n\n", len(pendientes))

	hechos, fallos := 0, 0
	for _, o := range pendientes {
		if err := svc.CrearPedido(ctx, o); err != nil {
			fallos++
			fmt.Printf("  ✗ %-16s %v\n", o.Numero, err)
			continue
		}
		hechos++
		fmt.Printf("  ✓ %-16s %s  %s\n", o.Numero, o.Canal, money(o.Total))
	}
	fmt.Printf("\n  Montados %d · con fallo %d\n", hechos, fallos)
	return nil
}

func money(v float64) string { return fmt.Sprintf("$ %.0f", v) }

// cmdConexiones administra las conexiones a Odoo y el trabajo que cuelga de
// cada una.
//
// Existe por un problema real: los productos se identifican por (conexión, id
// de plantilla en Odoo), así que apuntar Integra a otra instancia crea filas
// nuevas y deja huérfanos precios, descripciones, imágenes y atributos —horas
// de trabajo humano— colgando de la conexión vieja.
func cmdConexiones(ctx context.Context, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	accion := "listar"
	if len(args) > 0 {
		accion = args[0]
	}

	switch accion {
	case "listar":
		cs, err := st.ResumenConexiones(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("%-4s %-46s %-9s %8s %8s %8s\n", "ID", "INSTANCIA", "ESTADO", "PRODS", "PRECIOS", "IMÁGENES")
		for _, c := range cs {
			estado := "inactiva"
			if c.Activa {
				estado = "ACTIVA"
			}
			fmt.Printf("%-4d %-46s %-9s %8d %8d %8d\n",
				c.ID, recorta(c.BaseURL+" · "+c.Database, 46), estado,
				c.Productos, c.ConPrecio, c.ConImagen)
		}
		fmt.Printf("\nPara trasladar el trabajo de una a otra:\n")
		fmt.Printf("  integra conexiones migrar <origen> <destino>\n")
		return nil

	case "migrar":
		if len(args) < 3 {
			return fmt.Errorf("uso: integra conexiones migrar <origen> <destino> [--aplicar]")
		}
		origen, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("origen inválido: %s", args[1])
		}
		destino, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil {
			return fmt.Errorf("destino inválido: %s", args[2])
		}
		aplicar := false
		for _, a := range args[3:] {
			if a == "--aplicar" {
				aplicar = true
			}
		}

		res, err := st.MigrarTrabajo(ctx, origen, destino, !aplicar)
		if err != nil {
			return err
		}
		fmt.Printf("Emparejados por SKU  %d\n", res.Emparejados)
		fmt.Printf("Sin pareja           %d  (se quedan en la conexión de origen)\n\n", res.SinPareja)
		if res.Simulado {
			fmt.Printf("Se trasladarían:\n")
		} else {
			fmt.Printf("Trasladados:\n")
		}
		fmt.Printf("  Precios      %d\n", res.Precios)
		fmt.Printf("  Marcas       %d\n", res.Marcas)
		fmt.Printf("  Contenidos   %d\n", res.Contenidos)
		fmt.Printf("  Imágenes     %d productos\n", res.Imagenes)
		fmt.Printf("  Atributos    %d\n", res.Atributos)
		if res.Simulado {
			fmt.Printf("\nEsto fue una simulación. Para aplicarlo de verdad:\n")
			fmt.Printf("  integra conexiones migrar %d %d --aplicar\n", origen, destino)
		}
		return nil

	case "borrar":
		if len(args) < 2 {
			return fmt.Errorf("uso: integra conexiones borrar <id>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("identificador inválido: %s", args[1])
		}
		n, err := st.BorrarConexion(ctx, id)
		if err != nil {
			return err
		}
		fmt.Printf("Conexión %d borrada junto con %d productos.\n", id, n)
		return nil

	default:
		return fmt.Errorf("acción desconocida: %s (listar, migrar o borrar)", accion)
	}
}

// adaptadorFalabella construye el adaptador con la credencial guardada.
func adaptadorFalabella(ctx context.Context, st *store.Store, claveMaestra string) (*falabella.Adaptador, error) {
	cif, err := crypto.DesdeBase64(claveMaestra)
	if err != nil {
		return nil, err
	}
	cuentas, err := st.ListarCuentas(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range cuentas {
		if c.CanalCodigo != "falabella" {
			continue
		}
		_, cifrada, err := st.CredencialesDeCuenta(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		claro, err := cif.DescifrarTexto(cifrada)
		if err != nil {
			return nil, err
		}
		var campos map[string]string
		if err := json.Unmarshal([]byte(claro), &campos); err != nil {
			return nil, err
		}
		ad, err := channel.New(channel.Falabella, channel.Config{
			AccountID: c.ID, Credentials: campos,
		})
		if err != nil {
			return nil, err
		}
		fa, ok := ad.(*falabella.Adaptador)
		if !ok {
			return nil, fmt.Errorf("el adaptador de Falabella no es del tipo esperado")
		}
		return fa, nil
	}
	return nil, fmt.Errorf("la cuenta de Falabella no está conectada")
}

func cmdServe(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := registro(cfg.LogLevel)

	cif, err := crypto.DesdeBase64(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("INTEGRA_MASTER_KEY inválida: %w", err)
	}

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := comprobarEsquema(ctx, cfg.DatabaseURL); err != nil {
		return err
	}

	// La sincronización manual desde la interfaz reutiliza el mismo camino
	// que el subcomando sync.
	sincronizar := func(c context.Context) (string, error) {
		cli, conexionID, desde, err := abrirOdoo(c, st, cfg.MasterKey, false)
		if err != nil {
			return "", err
		}
		res, err := sync.Nuevo(cli, st, log).Catalogo(c, conexionID, desde)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d leídos, %d guardados", res.Leidos, res.Guardados), nil
	}

	// El banco de imágenes es opcional: sin directorio configurado, la API
	// responde 503 en esas rutas en vez de impedir que arranque el servidor.
	var alm *imagen.Almacen
	if cfg.ImageDir != "" {
		alm, err = imagen.NuevoAlmacen(cfg.ImageDir)
		if err != nil {
			return err
		}
		log.Info("banco de imágenes listo", "dir", cfg.ImageDir)
	}

	return api.Nuevo(st, log, cfg.HTTPAddr, alm, cif, jobs.NuevaCola(st.Pool()), sincronizar).
		ConInterfaz(cfg.WebDir).
		// La API es quien echa en falta al worker: el planificador, que avisa
		// de todo lo demás, corre dentro del worker y no puede avisar de que
		// el worker se murió. Por eso el despacho de correo se engancha
		// también aquí.
		ConVigilanciaDeProcesos(notificaciones.Nuevo(st, cif, log).Despachar).
		Escuchar(ctx)
}

func cmdWorker(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := registro(cfg.LogLevel)

	if err := comprobarEsquema(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	cif, err := crypto.DesdeBase64(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("INTEGRA_MASTER_KEY inválida: %w", err)
	}

	cola := jobs.NuevaCola(st.Pool())
	w := jobs.NuevoWorker(cola, log, cfg.WorkerConcurrency, 2*time.Second)

	// Los canales descargan las imágenes desde esta URL: tiene que ser
	// alcanzable desde internet cuando se publique de verdad.
	publicar.NuevoServicio(st, cif, log, cfg.PublicBaseURL).
		ConConciliacion(cfg.ConciliacionCada, cfg.RecrearPublicacionesCaidas).
		Registrar(w)

	// La ingesta de pedidos y su montaje en Odoo comparten worker: ambos son
	// llamadas a servicios ajenos que fallan y se reintentan igual.
	ordenes.NuevoServicio(st, cif, log, func(c context.Context) (*odoo.Client, error) {
		cli, _, _, err := abrirOdoo(c, st, cfg.MasterKey, false)
		return cli, err
	}).Registrar(w)

	if n, err := cola.Pendientes(ctx); err == nil && n > 0 {
		log.Info("hay trabajos esperando", "pendientes", n)
	}

	// El planificador corre junto al worker: es lo que hace que Integra
	// sincronice, publique e ingiera pedidos sin que nadie pulse un botón.
	plan := planificador.Nuevo(st, cola, log, cfg.SchedulerTick,
		func(c context.Context) (string, error) {
			cli, conexionID, desde, err := abrirOdoo(c, st, cfg.MasterKey, false)
			if err != nil {
				return "", err
			}
			res, err := sync.Nuevo(cli, st, log).Catalogo(c, conexionID, desde)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%d leídos, %d guardados", res.Leidos, res.Guardados), nil
		})

	// Las alertas salen por correo a los destinos configurados. Sin esto, el
	// módulo de alertas es un panel que nadie mira.
	plan = plan.ConNotificaciones(notificaciones.Nuevo(st, cif, log).Despachar)

	go func() {
		if err := plan.Ejecutar(ctx); err != nil {
			log.Error("el planificador se detuvo", "error", err)
		}
	}()

	return w.Ejecutar(ctx)
}

// comprobarEsquema falla pronto y con un mensaje claro si la base de datos no
// está migrada, en vez de reventar más tarde con un "relation does not exist".
func comprobarEsquema(ctx context.Context, dsn string) error {
	migs, err := migrate.Cargar(migrations.FS)
	if err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("conectando a PostgreSQL: %w", err)
	}
	defer conn.Close(context.Background())

	estados, err := migrate.Status(ctx, conn, migs)
	if err != nil {
		return fmt.Errorf("comprobando el esquema: %w", err)
	}
	var pendientes []string
	for _, e := range estados {
		if !e.Aplicada {
			pendientes = append(pendientes, e.Nombre)
		}
	}
	if len(pendientes) > 0 {
		return fmt.Errorf("hay %d migraciones sin aplicar (empezando por %s). Ejecuta: integra migrate up",
			len(pendientes), pendientes[0])
	}
	return nil
}

func cmdCrearUsuario(ctx context.Context, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("uso: integra crear-usuario <email> <nombre> <password> [rol (admin|operator|viewer)]")
	}

	email, nombre, password := args[0], args[1], args[2]
	rol := auth.RolOperator
	if len(args) >= 4 {
		rol = args[3]
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	id, err := st.CrearUsuario(ctx, email, nombre, hash, rol)
	if err != nil {
		return err
	}

	fmt.Printf("✓ Usuario %d creado con éxito (%s, rol=%s)\n", id, email, rol)
	return nil
}

func cmdListarUsuarios(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	usuarios, err := st.ListarUsuarios(ctx)
	if err != nil {
		return err
	}

	if len(usuarios) == 0 {
		fmt.Println("No hay usuarios registrados. Ejecuta: integra crear-usuario <email> <nombre> <password> admin")
		return nil
	}

	fmt.Printf("%-5s %-30s %-25s %-10s %s\n", "ID", "Email", "Nombre", "Rol", "Activo")
	fmt.Printf("%-5s %-30s %-25s %-10s %s\n", "──", "─────", "──────", "───", "──────")
	for _, u := range usuarios {
		activo := "sí"
		if !u.Active {
			activo = "no"
		}
		fmt.Printf("%-5d %-30s %-25s %-10s %s\n", u.ID, u.Email, u.Name, u.Role, activo)
	}
	return nil
}

// cmdCambiarUsuario da de baja o de alta a un usuario desde la línea de
// comandos. Hasta ahora la única forma de quitar a alguien era un DELETE a
// mano en PostgreSQL, que además no le cerraba la sesión.
//
// Desactivar y borrar comparten función porque comparten todo salvo la
// operación final: la misma búsqueda por correo, la misma negativa a dejar el
// sistema sin administradores y el mismo aviso sobre cuándo se corta el acceso.
func cmdCambiarUsuario(ctx context.Context, accion string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: integra %s-usuario <email>", accion)
	}
	email := args[0]

	confirmado := false
	for _, a := range args[1:] {
		if a == "--confirmar" {
			confirmado = true
		}
	}

	// Borrar pierde la autoría de lo que hizo el usuario y no se deshace.
	// Se pide decirlo dos veces, igual que en `conexiones migrar --aplicar`.
	if accion == "borrar" && !confirmado {
		fmt.Printf("Borrar a %s elimina su fila y deja sin autor lo que hizo:\n", email)
		fmt.Printf("los registros de auditoría y los precios que creó pasan a figurar\n")
		fmt.Printf("sin nadie detrás. No se puede deshacer.\n\n")
		fmt.Printf("Si solo quieres quitarle el acceso, esto es lo que buscas:\n")
		fmt.Printf("  integra desactivar-usuario %s\n\n", email)
		fmt.Printf("Y si de verdad quieres borrarlo:\n")
		fmt.Printf("  integra borrar-usuario %s --confirmar\n", email)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	var u *auth.Usuario
	switch accion {
	case "desactivar":
		u, err = st.DesactivarUsuario(ctx, email)
	case "activar":
		u, err = st.ActivarUsuario(ctx, email)
	case "borrar":
		u, err = st.BorrarUsuario(ctx, email)
	default:
		return fmt.Errorf("acción desconocida: %s", accion)
	}
	if err != nil {
		return err
	}

	switch accion {
	case "desactivar":
		if !u.Active {
			fmt.Printf("✓ %s ya estaba desactivado (rol=%s)\n", u.Email, u.Role)
			return nil
		}
		fmt.Printf("✓ %s desactivado (%s, rol=%s)\n", u.Email, u.Name, u.Role)
	case "activar":
		if u.Active {
			fmt.Printf("✓ %s ya estaba activo (rol=%s)\n", u.Email, u.Role)
			return nil
		}
		fmt.Printf("✓ %s activado de nuevo (%s, rol=%s)\n", u.Email, u.Name, u.Role)
		return nil
	case "borrar":
		fmt.Printf("✓ %s borrado (%s, rol=%s)\n", u.Email, u.Name, u.Role)
	}

	// El servidor comprueba la vigencia de cada sesión contra la base, pero se
	// guarda la respuesta unos segundos para no consultar en cada petición.
	// Decir cuánto tarda evita la duda de si el cambio ha surtido efecto.
	fmt.Printf("  Si tenía sesión abierta, deja de valer en menos de un minuto.\n")
	return nil
}

// cmdCambiarPassword pone una contraseña nueva a un usuario existente.
//
// Sin esto, una contraseña olvidada convierte la cuenta en un ladrillo: Integra
// no manda correos de recuperación y a la pantalla de cambio solo se llega
// habiendo entrado ya. La única salida era un UPDATE a mano en la base con un
// hash de bcrypt calculado aparte.
func cmdCambiarPassword(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: integra cambiar-password <email> [contraseña]\n\n" +
			"Si no pones la contraseña, se pide por teclado y no queda en el\n" +
			"historial del terminal, que es lo recomendable.")
	}
	email := args[0]

	var clave string
	if len(args) > 1 {
		clave = args[1]
		fmt.Println("Aviso: la contraseña queda guardada en el historial del terminal.")
		fmt.Println("Para evitarlo, ejecuta el comando sin ella y escríbela cuando la pida.")
		fmt.Println()
	} else {
		var err error
		clave, err = leerClaveOculta("Contraseña nueva: ")
		if err != nil {
			return err
		}
		repetida, err := leerClaveOculta("Repítela: ")
		if err != nil {
			return err
		}
		if clave != repetida {
			return fmt.Errorf("las dos contraseñas no coinciden")
		}
	}

	if len([]rune(clave)) < 8 {
		return fmt.Errorf("la contraseña debe tener al menos 8 caracteres")
	}

	hash, err := auth.HashPassword(clave)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	u, err := st.CambiarPassword(ctx, email, hash)
	if err != nil {
		return err
	}

	fmt.Printf("\n✓ Contraseña cambiada para %s (%s, rol=%s)\n", u.Email, u.Name, u.Role)
	if !u.Active {
		// Cambiar la clave de una cuenta desactivada no la reactiva, y quien lo
		// hace casi siempre espera poder entrar acto seguido.
		fmt.Printf("\nOjo: esta cuenta está DESACTIVADA y aún no podrá entrar.\n")
		fmt.Printf("  integra activar-usuario %s\n", u.Email)
	}
	return nil
}

// leerClaveOculta pide una contraseña sin mostrarla mientras se escribe.
//
// Si el terminal no lo permite —una tubería, una tarea programada— se avisa en
// vez de leerla en claro sin decir nada: el usuario cree que está oculta.
func leerClaveOculta(aviso string) (string, error) {
	fmt.Print(aviso)
	fd := int(syscall.Stdin)
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("\nno hay terminal interactivo para pedir la contraseña; " +
			"pásala como argumento: integra cambiar-password <email> <contraseña>")
	}
	b, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("leyendo la contraseña: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}
