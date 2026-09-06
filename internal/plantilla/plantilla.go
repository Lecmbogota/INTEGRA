// Package plantilla genera y lee el archivo de actualización masiva.
//
// Es el camino para cambiar cientos de precios y promociones sin abrir
// cientos de fichas: Integra escribe una hoja con el estado actual, el
// operador la edita en Excel y la sube; Integra la valida entera antes de
// tocar nada y luego aplica.
//
// La hoja es .xlsx y no .csv a propósito. Un CSV abierto en un Excel en
// español parte por punto y coma, interpreta 1.500 como mil quinientos o como
// uno coma cinco según la máquina, y destroza los acentos si falta el BOM. Con
// xlsx el número es un número y el texto es texto, en cualquier equipo.
package plantilla

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// Hoja donde vive la plantilla. El nombre importa: al leer se busca por él
// para que un archivo con hojas añadidas por el operador siga sirviendo.
const Hoja = "Precios"

// hojaAyuda documenta el formato dentro del propio archivo, que es donde el
// operador va a mirar cuando algo no le cuadre.
const hojaAyuda = "Instrucciones"

// Columnas de la plantilla, en orden. Las de referencia van primero para que
// se lean como una lista de precios; las editables después.
var columnas = []struct {
	titulo   string
	ancho    float64
	editable bool
	ayuda    string
}{
	{"SKU", 22, false, "Referencia del producto. NO la modifiques: es lo que enlaza cada fila con Integra."},
	{"Producto", 52, false, "Nombre en Odoo. Solo para orientarte."},
	{"Marca", 18, false, "Solo para orientarte."},
	{"Stock", 10, false, "Unidades disponibles hoy. Solo para orientarte."},
	{"Precio actual", 15, false, "Lo que vale hoy en Integra. Solo para orientarte."},
	{"Precio nuevo", 15, true, "Escribe aqui el precio nuevo. Dejalo VACIO si no quieres cambiarlo."},
	{"Canal promocion", 18, true, "mercadolibre, falabella, woocommerce o shopify. Vacio = no tocar la promocion."},
	{"Precio promocion", 17, true, "Precio rebajado. Tiene que ser menor que el precio de venta."},
	{"Promocion empieza", 20, true, "Fecha y hora de inicio (dd/mm/aaaa hh:mm). Vacio = empieza al cargar."},
	{"Promocion termina", 20, true, "Fecha y hora de fin. Vacio = sin fecha de fin."},
}

// Fila es lo que Integra escribe en la plantilla.
type Fila struct {
	SKU          string
	Nombre       string
	Marca        string
	Stock        float64
	Precio       *float64
	PromoCanal   string
	PromoPrecio  *float64
	PromoInicia  *time.Time
	PromoTermina *time.Time
}

// Generar escribe la plantilla con el catálogo dado.
func Generar(w io.Writer, filas []Fila) error {
	f := excelize.NewFile()
	defer f.Close()

	idx, err := f.NewSheet(Hoja)
	if err != nil {
		return err
	}
	f.SetActiveSheet(idx)
	if err := f.DeleteSheet("Sheet1"); err != nil {
		return err
	}

	encabezado, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"1E3A5F"}},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	if err != nil {
		return err
	}
	// Las columnas que se editan van en otro color: sin eso, el operador tiene
	// que recordar cuáles puede tocar, y acaba escribiendo en las de referencia.
	encabezadoEdit, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "1B3A1B", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"C6E7C6"}},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	if err != nil {
		return err
	}
	moneda, err := f.NewStyle(&excelize.Style{CustomNumFmt: strPtr(`#,##0`)})
	if err != nil {
		return err
	}
	gris, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Color: "6B7280"},
	})
	if err != nil {
		return err
	}
	fechaEstilo, err := f.NewStyle(&excelize.Style{CustomNumFmt: strPtr(`dd/mm/yyyy hh:mm`)})
	if err != nil {
		return err
	}

	for i, c := range columnas {
		col, _ := excelize.ColumnNumberToName(i + 1)
		celda := col + "1"
		if err := f.SetCellStr(Hoja, celda, c.titulo); err != nil {
			return err
		}
		estilo := encabezado
		if c.editable {
			estilo = encabezadoEdit
		}
		if err := f.SetCellStyle(Hoja, celda, celda, estilo); err != nil {
			return err
		}
		if err := f.SetColWidth(Hoja, col, col, c.ancho); err != nil {
			return err
		}
		// El comentario deja la ayuda a un clic de distancia de la casilla.
		_ = f.AddComment(Hoja, excelize.Comment{
			Cell:   celda,
			Author: "Integra",
			Paragraph: []excelize.RichTextRun{
				{Text: c.titulo + ": ", Font: &excelize.Font{Bold: true}},
				{Text: c.ayuda},
			},
		})
	}
	if err := f.SetRowHeight(Hoja, 1, 30); err != nil {
		return err
	}

	for i, fila := range filas {
		n := i + 2
		poner := func(colIdx int, v any) error {
			col, _ := excelize.ColumnNumberToName(colIdx)
			return f.SetCellValue(Hoja, fmt.Sprintf("%s%d", col, n), v)
		}
		if err := poner(1, fila.SKU); err != nil {
			return err
		}
		if err := poner(2, fila.Nombre); err != nil {
			return err
		}
		if err := poner(3, fila.Marca); err != nil {
			return err
		}
		if err := poner(4, fila.Stock); err != nil {
			return err
		}
		if fila.Precio != nil {
			if err := poner(5, *fila.Precio); err != nil {
				return err
			}
		}
		// La columna 6 (precio nuevo) se deja vacía a propósito: si viniera
		// rellena con el precio actual, guardar sin tocar nada reescribiría
		// todo el catálogo y borraría la fecha de «precio actualizado».
		if fila.PromoCanal != "" {
			if err := poner(7, fila.PromoCanal); err != nil {
				return err
			}
		}
		if fila.PromoPrecio != nil {
			if err := poner(8, *fila.PromoPrecio); err != nil {
				return err
			}
		}
		if fila.PromoInicia != nil {
			if err := poner(9, *fila.PromoInicia); err != nil {
				return err
			}
		}
		if fila.PromoTermina != nil {
			if err := poner(10, *fila.PromoTermina); err != nil {
				return err
			}
		}
	}

	if n := len(filas); n > 0 {
		_ = f.SetCellStyle(Hoja, "A2", fmt.Sprintf("D%d", n+1), gris)
		_ = f.SetCellStyle(Hoja, "E2", fmt.Sprintf("E%d", n+1), gris)
		_ = f.SetCellStyle(Hoja, "F2", fmt.Sprintf("F%d", n+1), moneda)
		_ = f.SetCellStyle(Hoja, "H2", fmt.Sprintf("H%d", n+1), moneda)
		_ = f.SetCellStyle(Hoja, "I2", fmt.Sprintf("J%d", n+1), fechaEstilo)
	}

	// Fila de encabezado congelada: con 500 filas, sin esto se pierde de vista
	// qué columna se está editando en cuanto se hace scroll.
	if err := f.SetPanes(Hoja, &excelize.Panes{
		Freeze: true, Split: false, XSplit: 0, YSplit: 1,
		TopLeftCell: "A2", ActivePane: "bottomLeft",
	}); err != nil {
		return err
	}

	if err := escribirAyuda(f); err != nil {
		return err
	}
	return f.Write(w)
}

func escribirAyuda(f *excelize.File) error {
	if _, err := f.NewSheet(hojaAyuda); err != nil {
		return err
	}
	if err := f.SetColWidth(hojaAyuda, "A", "A", 110); err != nil {
		return err
	}
	titulo, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 13}})
	if err != nil {
		return err
	}
	texto, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"},
	})
	if err != nil {
		return err
	}

	lineas := []string{
		"Como usar esta plantilla",
		"",
		"1. Edita SOLO las columnas con encabezado VERDE. Las grises son de referencia.",
		"2. Deja vacia cualquier casilla que no quieras cambiar. Vacio significa \"no tocar\", no \"borrar\".",
		"3. No cambies ni borres la columna SKU: es lo que enlaza cada fila con el producto.",
		"4. Puedes borrar las filas de productos que no vayas a tocar; solo se procesa lo que subas.",
		"5. Guarda el archivo en formato Excel (.xlsx) y subelo en Integra.",
		"",
		"Precios",
		"",
		"Escribe el numero sin puntos ni simbolo de moneda: 129900, no $ 129.900.",
		"Los precios son en pesos colombianos.",
		"",
		"Promociones",
		"",
		"Para poner una promocion necesitas rellenar Canal promocion y Precio promocion.",
		"El canal se escribe en minusculas: mercadolibre, falabella, woocommerce o shopify.",
		"El precio de promocion tiene que ser menor que el precio de venta.",
		"",
		"Si dejas \"Promocion empieza\" vacia, la promocion arranca en cuanto cargues el archivo.",
		"Si dejas \"Promocion termina\" vacia, la promocion no caduca hasta que la canceles a mano.",
		"Las fechas se escriben dd/mm/aaaa hh:mm. Si solo pones la fecha, se entiende a las 00:00.",
		"",
		"Integra aplica la rebaja cuando llega la hora de inicio y devuelve el precio normal cuando",
		"llega la de fin, sin que tengas que volver a entrar.",
		"",
		"Antes de aplicar nada",
		"",
		"Al subir el archivo, Integra te muestra primero un resumen de lo que va a cambiar y la lista",
		"de errores por fila. Nada se guarda hasta que lo confirmas.",
	}
	for i, l := range lineas {
		celda := fmt.Sprintf("A%d", i+1)
		if err := f.SetCellStr(hojaAyuda, celda, l); err != nil {
			return err
		}
		estilo := texto
		if i == 0 || l == "Precios" || l == "Promociones" || l == "Antes de aplicar nada" {
			estilo = titulo
		}
		if err := f.SetCellStyle(hojaAyuda, celda, celda, estilo); err != nil {
			return err
		}
	}
	return nil
}

// FilaLeida es una línea del archivo que subió el operador, ya interpretada.
//
// Los punteros distinguen "vacío" de "cero", que en una plantilla es la
// diferencia entre no tocar el precio y regalarlo.
type FilaLeida struct {
	Numero       int // número de fila en Excel, para poder señalarla en los errores
	SKU          string
	Precio       *float64
	PromoCanal   string
	PromoPrecio  *float64
	PromoInicia  *time.Time
	PromoTermina *time.Time
}

// Leer interpreta el archivo subido.
//
// Devuelve las filas con contenido y los errores de formato encontrados; una
// fila con error no sale en la lista, así que quien llame no puede aplicarla
// por descuido.
func Leer(r io.Reader) ([]FilaLeida, []Problema, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, nil, fmt.Errorf("el archivo no se pudo abrir como Excel (.xlsx): %w", err)
	}
	defer f.Close()

	hoja := Hoja
	if _, err := f.GetSheetIndex(hoja); err != nil || !tieneHoja(f, hoja) {
		// Si el operador renombró la hoja, se usa la primera en vez de fallar.
		hojas := f.GetSheetList()
		if len(hojas) == 0 {
			return nil, nil, fmt.Errorf("el archivo no tiene ninguna hoja")
		}
		hoja = hojas[0]
	}

	filas, err := f.GetRows(hoja)
	if err != nil {
		return nil, nil, fmt.Errorf("leyendo la hoja %q: %w", hoja, err)
	}
	if len(filas) < 2 {
		return nil, nil, fmt.Errorf("la hoja %q no tiene filas de datos", hoja)
	}

	// Las columnas se localizan por su encabezado, no por posición: así el
	// archivo sigue sirviendo si alguien reordena o inserta una columna suya.
	pos := map[string]int{}
	for i, t := range filas[0] {
		pos[normalizarTitulo(t)] = i
	}
	col := func(nombre string) int {
		if i, ok := pos[normalizarTitulo(nombre)]; ok {
			return i
		}
		return -1
	}
	iSKU, iPrecio := col("SKU"), col("Precio nuevo")
	iCanal, iPromo := col("Canal promocion"), col("Precio promocion")
	iInicia, iTermina := col("Promocion empieza"), col("Promocion termina")

	if iSKU < 0 {
		return nil, nil, fmt.Errorf("no encuentro la columna SKU: ¿es esta la plantilla que descargaste de Integra?")
	}

	var out []FilaLeida
	var problemas []Problema

	for n, fila := range filas[1:] {
		numero := n + 2
		celda := func(i int) string {
			if i < 0 || i >= len(fila) {
				return ""
			}
			return strings.TrimSpace(fila[i])
		}

		sku := celda(iSKU)
		precioTxt, canalTxt := celda(iPrecio), strings.ToLower(celda(iCanal))
		promoTxt := celda(iPromo)
		iniciaTxt, terminaTxt := celda(iInicia), celda(iTermina)

		// Una fila sin nada editado no es un error: es la mayoría del archivo.
		if precioTxt == "" && canalTxt == "" && promoTxt == "" &&
			iniciaTxt == "" && terminaTxt == "" {
			continue
		}
		if sku == "" {
			problemas = append(problemas, Problema{numero, "", "la fila tiene datos pero no tiene SKU"})
			continue
		}

		l := FilaLeida{Numero: numero, SKU: sku, PromoCanal: canalTxt}
		malo := false

		if precioTxt != "" {
			v, err := numero_(precioTxt)
			if err != nil {
				problemas = append(problemas, Problema{numero, sku,
					fmt.Sprintf("precio nuevo %q no es un número", precioTxt)})
				malo = true
			} else if v <= 0 {
				problemas = append(problemas, Problema{numero, sku, "el precio nuevo tiene que ser mayor que cero"})
				malo = true
			} else {
				l.Precio = &v
			}
		}
		if promoTxt != "" {
			v, err := numero_(promoTxt)
			if err != nil {
				problemas = append(problemas, Problema{numero, sku,
					fmt.Sprintf("precio de promoción %q no es un número", promoTxt)})
				malo = true
			} else if v <= 0 {
				problemas = append(problemas, Problema{numero, sku, "el precio de promoción tiene que ser mayor que cero"})
				malo = true
			} else {
				l.PromoPrecio = &v
			}
		}
		if iniciaTxt != "" {
			t, err := fecha(iniciaTxt)
			if err != nil {
				problemas = append(problemas, Problema{numero, sku,
					fmt.Sprintf("«promoción empieza» %q no es una fecha (usa dd/mm/aaaa hh:mm)", iniciaTxt)})
				malo = true
			} else {
				l.PromoInicia = &t
			}
		}
		if terminaTxt != "" {
			t, err := fecha(terminaTxt)
			if err != nil {
				problemas = append(problemas, Problema{numero, sku,
					fmt.Sprintf("«promoción termina» %q no es una fecha (usa dd/mm/aaaa hh:mm)", terminaTxt)})
				malo = true
			} else {
				l.PromoTermina = &t
			}
		}

		// Una promoción a medias es el error más caro de la plantilla: sin
		// canal no se sabe dónde rebajar, y sin precio no hay rebaja. Se
		// rechaza aquí en vez de aplicar la mitad.
		promoAlgo := canalTxt != "" || promoTxt != "" || iniciaTxt != "" || terminaTxt != ""
		if promoAlgo {
			if canalTxt == "" {
				problemas = append(problemas, Problema{numero, sku,
					"pusiste datos de promoción pero dejaste vacío el canal"})
				malo = true
			}
			if promoTxt == "" {
				problemas = append(problemas, Problema{numero, sku,
					"pusiste datos de promoción pero dejaste vacío el precio de promoción"})
				malo = true
			}
		}
		if l.PromoInicia != nil && l.PromoTermina != nil && !l.PromoTermina.After(*l.PromoInicia) {
			problemas = append(problemas, Problema{numero, sku,
				"la promoción termina antes de empezar"})
			malo = true
		}

		if !malo {
			out = append(out, l)
		}
	}
	return out, problemas, nil
}

// Problema es un error atribuido a una fila concreta del archivo.
type Problema struct {
	Fila    int    `json:"fila"`
	SKU     string `json:"sku"`
	Mensaje string `json:"mensaje"`
}

func tieneHoja(f *excelize.File, nombre string) bool {
	for _, h := range f.GetSheetList() {
		if h == nombre {
			return true
		}
	}
	return false
}

// normalizarTitulo hace que "Precio Promoción" y "precio promocion" sean la
// misma columna: el archivo pasa por Excel, por correo y por las manos de
// quien sea, y los acentos no sobreviven ese viaje de forma fiable.
func normalizarTitulo(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	r := strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ñ", "n")
	s = r.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// numero_ acepta lo que de verdad sale de una hoja de cálculo colombiana:
// 129900, 129.900, 129,900, $ 129.900 y 129900,50.
func numero_(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("$", "", " ", "", " ", "").Replace(s)
	if s == "" {
		return 0, fmt.Errorf("vacío")
	}

	ultimaComa := strings.LastIndex(s, ",")
	ultimoPunto := strings.LastIndex(s, ".")

	switch {
	case ultimaComa >= 0 && ultimoPunto >= 0:
		// El separador decimal es el que va más a la derecha; el otro agrupa
		// miles. Así 1.234,56 y 1,234.56 se leen los dos bien.
		if ultimaComa > ultimoPunto {
			s = strings.ReplaceAll(s, ".", "")
			s = strings.Replace(s, ",", ".", 1)
		} else {
			s = strings.ReplaceAll(s, ",", "")
		}
	case ultimaComa >= 0:
		// Una sola coma: decimal si deja 1 o 2 cifras detrás, miles si deja 3.
		if len(s)-ultimaComa-1 == 3 {
			s = strings.ReplaceAll(s, ",", "")
		} else {
			s = strings.Replace(s, ",", ".", 1)
		}
	case ultimoPunto >= 0:
		// Mismo criterio con el punto. En pesos, 129.900 es siempre miles.
		if len(s)-ultimoPunto-1 == 3 {
			s = strings.ReplaceAll(s, ".", "")
		}
	}
	return strconv.ParseFloat(s, 64)
}

// formatos aceptados al leer fechas. El día va delante porque es lo que
// escribe cualquiera en Colombia; el formato ISO se acepta porque es lo que
// devuelve Excel cuando la casilla ya era una fecha de verdad.
var formatos = []string{
	"02/01/2006 15:04", "02/01/2006 15:04:05", "02/01/2006",
	"02-01-2006 15:04", "02-01-2006",
	"2006-01-02 15:04", "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05", "2006-01-02",
	"01/02/06 15:04", // Excel en inglés al convertir a texto
}

func fecha(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	// Excel guarda las fechas como número de serie; si la casilla venía con
	// formato de fecha, GetRows a veces devuelve ese número en crudo.
	if n, err := strconv.ParseFloat(s, 64); err == nil && n > 20000 && n < 80000 {
		t, err := excelize.ExcelDateToTime(n, false)
		if err != nil {
			return time.Time{}, err
		}
		return enHorarioLocal(t), nil
	}
	for _, f := range formatos {
		if t, err := time.ParseInLocation(f, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("formato de fecha no reconocido")
}

// enHorarioLocal reinterpreta una fecha de Excel —que no lleva zona— como hora
// local. Sin esto, una promoción puesta para las 00:00 empezaría cinco horas
// antes de lo que el operador escribió.
func enHorarioLocal(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
}

func strPtr(s string) *string { return &s }
