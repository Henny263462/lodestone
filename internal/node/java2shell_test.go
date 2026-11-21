package node

import "testing"

func TestJavaToolOptionsMitigatesJava2Shell(t *testing.T) {
	cases := []struct {
		name string
		java int
		user string
		want string
	}{
		{"Java 21 ohne eigene Optionen", 21, "", "-XX:-UseBASE64Intrinsics"},
		{"Java 17 mit eigenen Optionen", 17, "-Dfile.encoding=UTF-8", "-XX:-UseBASE64Intrinsics -Dfile.encoding=UTF-8"},
		{"Java 8 ist nicht betroffen", 8, "", ""},
		{"Java 8 behält eigene Optionen", 8, "-Dx=1", "-Dx=1"},
		{"eigenes Image: altes Java darf nicht scheitern", 0, "", "-XX:+IgnoreUnrecognizedVMOptions -XX:-UseBASE64Intrinsics"},
		{"bewusste Wahl des Benutzers bleibt", 21, "-XX:+UseBASE64Intrinsics", "-XX:+UseBASE64Intrinsics"},
	}
	for _, c := range cases {
		if got := javaToolOptions(c.java, c.user); got != c.want {
			t.Errorf("%s: %q, erwartet %q", c.name, got, c.want)
		}
	}
}
