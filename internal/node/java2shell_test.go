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
		{"ein Teilwort schaltet nichts ab", 21, "-Dx=UseBASE64Intrinsics", "-XX:-UseBASE64Intrinsics -Dx=UseBASE64Intrinsics"},
		{"ähnliche Option schaltet nichts ab", 21, "-XX:+UseBASE64IntrinsicsX", "-XX:-UseBASE64Intrinsics -XX:+UseBASE64IntrinsicsX"},
	}
	for _, c := range cases {
		if got := javaToolOptions(c.java, c.user); got != c.want {
			t.Errorf("%s: %q, erwartet %q", c.name, got, c.want)
		}
	}
}

// Die Abschaltung über _JAVA_OPTIONS, JDK_JAVA_OPTIONS oder die Startzeile gewinnt gegen JAVA_TOOL_OPTIONS. Sie bleibt
// erlaubt, aber die Konsole sagt es beim Start.
func TestJava2ShellOptOutIsNamed(t *testing.T) {
	cases := []struct {
		name      string
		container ContainerSpec
		want      string
	}{
		{"geschützt", ContainerSpec{Env: []string{"JAVA_TOOL_OPTIONS=-XX:-UseBASE64Intrinsics"}, Cmd: []string{"/bin/sh", "-c", "java -jar server.jar"}}, ""},
		{"_JAVA_OPTIONS", ContainerSpec{Env: []string{"JAVA_TOOL_OPTIONS=-XX:-UseBASE64Intrinsics", "_JAVA_OPTIONS=-XX:+UseBASE64Intrinsics"}}, "_JAVA_OPTIONS"},
		{"JDK_JAVA_OPTIONS", ContainerSpec{Env: []string{"JDK_JAVA_OPTIONS=-Xss1m -XX:+UseBASE64Intrinsics"}}, "JDK_JAVA_OPTIONS"},
		{"Startzeile", ContainerSpec{Cmd: []string{"/bin/sh", "-c", "java -XX:+UseBASE64Intrinsics -jar server.jar"}}, "der Startzeile"},
		{"Teilwort zählt nicht", ContainerSpec{Env: []string{"X=-Dy=-XX:+UseBASE64Intrinsics"}}, ""},
	}
	for _, c := range cases {
		if got := java2ShellDisabledIn(c.container); got != c.want {
			t.Errorf("%s: %q, erwartet %q", c.name, got, c.want)
		}
	}
}
