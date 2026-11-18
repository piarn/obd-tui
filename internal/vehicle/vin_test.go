package vehicle

import "testing"

func TestDecode(t *testing.T) {
	cases := []struct {
		vin   string
		name  string
		group Group
		year  int
	}{
		{"TMBJJ7NE8L0123456", "Skoda", GroupVAG, 2020},
		{"WBA8E9C50GK123456", "BMW", GroupBMW, 2016},
		{"SHHFK7H50JU123456", "Honda", GroupHonda, 2018},
		{"JTDKN3DU5A0123456", "Toyota", GroupGeneric, 2010},
		{"XYZ", "", GroupGeneric, 0},
		{"", "", GroupGeneric, 0},
	}
	for _, c := range cases {
		mk, y := Decode(c.vin)
		if mk.Name != c.name || mk.Group != c.group || y != c.year {
			t.Errorf("%s: got %+v %d", c.vin, mk, y)
		}
	}
}
