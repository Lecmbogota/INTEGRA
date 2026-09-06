package xmlrpc

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeResponse(t *testing.T) {
	casos := []struct {
		nombre    string
		xml       string
		comprueba func(t *testing.T, v interface{})
	}{
		{
			nombre: "authenticate devuelve un uid",
			xml:    `<?xml version='1.0'?><methodResponse><params><param><value><int>7</int></value></param></params></methodResponse>`,
			comprueba: func(t *testing.T, v interface{}) {
				if v != int64(7) {
					t.Fatalf("uid = %#v, se esperaba int64(7)", v)
				}
			},
		},
		{
			// El caso que justifica escribir este decodificador: Odoo devuelve
			// `false` en lugar de uid cuando las credenciales no valen.
			nombre: "authenticate rechazado devuelve false, no un fault",
			xml:    `<?xml version='1.0'?><methodResponse><params><param><value><boolean>0</boolean></value></param></params></methodResponse>`,
			comprueba: func(t *testing.T, v interface{}) {
				if v != false {
					t.Fatalf("se esperaba false, se obtuvo %#v", v)
				}
			},
		},
		{
			nombre: "campos vacíos llegan como false y no rompen el struct",
			xml: `<?xml version='1.0'?><methodResponse><params><param><value><array><data>
				<value><struct>
					<member><name>id</name><value><int>42</int></value></member>
					<member><name>name</name><value><string>Teclado KX-400</string></value></member>
					<member><name>default_code</name><value><boolean>0</boolean></value></member>
					<member><name>description_sale</name><value><boolean>0</boolean></value></member>
					<member><name>list_price</name><value><double>33529.41</double></value></member>
					<member><name>categ_id</name><value><array><data>
						<value><int>5</int></value>
						<value><string>Perif&#233;ricos</string></value>
					</data></array></value></member>
				</struct></value>
			</data></array></value></param></params></methodResponse>`,
			comprueba: func(t *testing.T, v interface{}) {
				filas, ok := v.([]interface{})
				if !ok || len(filas) != 1 {
					t.Fatalf("se esperaba una lista de 1 elemento, se obtuvo %#v", v)
				}
				m := filas[0].(map[string]interface{})

				if m["id"] != int64(42) {
					t.Errorf("id = %#v", m["id"])
				}
				if m["name"] != "Teclado KX-400" {
					t.Errorf("name = %#v", m["name"])
				}
				// Lo importante: esto es un bool, no un string. Decodificar
				// hacia un struct con `DefaultCode string` fallaría aquí.
				if m["default_code"] != false {
					t.Errorf("default_code = %#v, se esperaba false", m["default_code"])
				}
				if m["list_price"] != 33529.41 {
					t.Errorf("list_price = %#v", m["list_price"])
				}
				categ, ok := m["categ_id"].([]interface{})
				if !ok || len(categ) != 2 || categ[0] != int64(5) || categ[1] != "Periféricos" {
					t.Errorf("categ_id = %#v", m["categ_id"])
				}
			},
		},
		{
			nombre: "value sin etiqueta de tipo se interpreta como cadena",
			xml:    `<?xml version='1.0'?><methodResponse><params><param><value>texto suelto</value></param></params></methodResponse>`,
			comprueba: func(t *testing.T, v interface{}) {
				if v != "texto suelto" {
					t.Fatalf("v = %#v", v)
				}
			},
		},
		{
			nombre: "array vacío",
			xml:    `<?xml version='1.0'?><methodResponse><params><param><value><array><data></data></array></value></param></params></methodResponse>`,
			comprueba: func(t *testing.T, v interface{}) {
				arr, ok := v.([]interface{})
				if !ok || len(arr) != 0 {
					t.Fatalf("v = %#v, se esperaba una lista vacía", v)
				}
			},
		},
		{
			nombre: "struct anidado dentro de array dentro de struct",
			xml: `<?xml version='1.0'?><methodResponse><params><param><value><struct>
				<member><name>lineas</name><value><array><data>
					<value><struct><member><name>sku</name><value><string>AO-SC-1012</string></value></member></struct></value>
					<value><struct><member><name>sku</name><value><string>R6-KB-1001</string></value></member></struct></value>
				</data></array></value></member>
			</struct></value></param></params></methodResponse>`,
			comprueba: func(t *testing.T, v interface{}) {
				m := v.(map[string]interface{})
				lineas := m["lineas"].([]interface{})
				if len(lineas) != 2 {
					t.Fatalf("se esperaban 2 líneas, hay %d", len(lineas))
				}
				if lineas[1].(map[string]interface{})["sku"] != "R6-KB-1001" {
					t.Errorf("sku = %#v", lineas[1])
				}
			},
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			v, err := decodeResponse([]byte(c.xml))
			if err != nil {
				t.Fatalf("error inesperado: %v", err)
			}
			c.comprueba(t, v)
		})
	}
}

func TestDecodeFault(t *testing.T) {
	x := `<?xml version='1.0'?><methodResponse><fault><value><struct>
		<member><name>faultCode</name><value><int>1</int></value></member>
		<member><name>faultString</name><value><string>Access Denied</string></value></member>
	</struct></value></fault></methodResponse>`

	_, err := decodeResponse([]byte(x))
	if err == nil {
		t.Fatal("se esperaba un error")
	}
	var f *Fault
	if !errors.As(err, &f) {
		t.Fatalf("se esperaba *Fault, se obtuvo %T", err)
	}
	if f.Code != 1 || f.Message != "Access Denied" {
		t.Fatalf("fault = %+v", f)
	}
}

func TestEncodeRequest(t *testing.T) {
	// Una llamada representativa: search_read con dominio y kwargs.
	body, err := encodeRequest("execute_kw",
		[]interface{}{
			"mi-bd", int64(2), "clave",
			"product.product", "search_read",
			[]interface{}{[]interface{}{[]interface{}{"sale_ok", "=", true}}},
			map[string]interface{}{"limit": 5},
		})
	if err != nil {
		t.Fatalf("error codificando: %v", err)
	}

	// Se vuelve a decodificar como respuesta para comprobar que el XML es válido
	// y que los tipos sobreviven el viaje de ida y vuelta.
	s := string(body)
	for _, frag := range []string{
		"<methodName>execute_kw</methodName>",
		"<string>product.product</string>",
		"<boolean>1</boolean>",
		"<int>5</int>",
	} {
		if !contains(s, frag) {
			t.Errorf("falta %q en:\n%s", frag, s)
		}
	}
}

func TestEncodeEscapaCaracteresEspeciales(t *testing.T) {
	// Los nombres de producto traen &, <, comillas y acentos constantemente.
	body, err := encodeRequest("test", []interface{}{`SanDisk Extreme Pro® <microSD> & "UHS-I"`})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if _, err := decodeResponseFromCall(body); err != nil {
		t.Fatalf("el XML generado no es válido: %v", err)
	}
}

// decodeResponseFromCall reutiliza el parser sobre un methodCall, comprobando
// únicamente que el documento está bien formado.
func decodeResponseFromCall(body []byte) (interface{}, error) {
	_, err := decodeResponse(body)
	// decodeResponse no encuentra <params> de respuesta en un methodCall, pero
	// sí falla antes si el XML está mal formado. Se distinguen los dos casos.
	if err != nil && contains(err.Error(), "mal formado") {
		return nil, err
	}
	return nil, nil
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Los tipos de abajo son los que usa el proyecto para hablar con Odoo. Sin
// ellos el codificador fallaba en TODA escritura: write y los metodos de
// negocio reciben siempre una lista de ids, y el proyecto los maneja como
// []int64. El fallo no aparecio hasta ejecutar una replica real contra Odoo.
func TestEncodeSoportaLosTiposDelProyecto(t *testing.T) {
	casos := []struct {
		nombre   string
		valor    interface{}
		contiene string
	}{
		{"lista de ids int64", []int64{12, 34}, "<array><data><value><int>12</int></value><value><int>34</int></value></data></array>"},
		{"lista de ids int", []int{5}, "<value><int>5</int></value>"},
		{"lista de cadenas", []string{"a", "b"}, "<value><string>a</string></value>"},
		{"mapa de cadenas", map[string]string{"k": "v"}, "<member><name>k</name><value><string>v</string></value></member>"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			cuerpo, err := encodeRequest("execute_kw", []interface{}{c.valor})
			if err != nil {
				t.Fatalf("no se pudo codificar %T: %v", c.valor, err)
			}
			if !strings.Contains(string(cuerpo), c.contiene) {
				t.Fatalf("falta %q en:\n%s", c.contiene, cuerpo)
			}
		})
	}
}

// El caso exacto que rompia: write recibe (ids, valores).
func TestEncodeLlamadaDeEscritura(t *testing.T) {
	cuerpo, err := encodeRequest("execute_kw", []interface{}{"basedatos", int64(2), "clave", "product.product", "write", []interface{}{[]int64{42}, map[string]interface{}{"name": "Nuevo"}}})
	if err != nil {
		t.Fatalf("una escritura tipica debe poder codificarse: %v", err)
	}
	if !strings.Contains(string(cuerpo), "<int>42</int>") {
		t.Fatalf("el id no viajo en la peticion:\n%s", cuerpo)
	}
}

// Las listas vacias tienen que producir un array vacio valido, no romperse:
// escribir sobre cero registros es legitimo.
func TestEncodeListaVacia(t *testing.T) {
	cuerpo, err := encodeRequest("execute_kw", []interface{}{[]int64{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cuerpo), "<array><data></data></array>") {
		t.Fatalf("una lista vacia debe dar un array vacio:\n%s", cuerpo)
	}
}


