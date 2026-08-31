package sessions

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileQuery(t *testing.T) {
	t.Run("fixed string folds case and escapes metachars", func(t *testing.T) {
		re, err := CompileQuery("a+b (X)", false, false)
		require.NoError(t, err)
		assert.True(t, re.MatchString("says A+B (x) here"))
		assert.False(t, re.MatchString("aab x"))
	})

	t.Run("case sensitive", func(t *testing.T) {
		re, err := CompileQuery("Auth", false, true)
		require.NoError(t, err)
		assert.True(t, re.MatchString("Auth test"))
		assert.False(t, re.MatchString("auth test"))
	})

	t.Run("regex mode", func(t *testing.T) {
		re, err := CompileQuery("a{2,3}b", true, false)
		require.NoError(t, err)
		assert.True(t, re.MatchString("xAABz"))
		assert.False(t, re.MatchString("ab"))
	})

	t.Run("invalid regex", func(t *testing.T) {
		_, err := CompileQuery("(", true, false)
		assert.Error(t, err)
	})
}

func TestSnippet(t *testing.T) {
	t.Run("short text passes through collapsed", func(t *testing.T) {
		re, _ := CompileQuery("flaky", false, false)
		loc := re.FindStringIndex("the   auth\ttest is\nflaky on CI")
		got := Snippet("the   auth\ttest is\nflaky on CI", loc)
		assert.Equal(t, "the auth test is flaky on CI", got)
	})

	t.Run("long text clips with ellipses around the match", func(t *testing.T) {
		text := strings.Repeat("aaaa ", 200) + "NEEDLE" + strings.Repeat(" bbbb", 200)
		re, _ := CompileQuery("needle", false, false)
		loc := re.FindStringIndex(text)
		got := Snippet(text, loc)
		assert.Contains(t, got, "NEEDLE")
		assert.LessOrEqual(t, utf8.RuneCountInString(got), 200)
		assert.True(t, strings.HasPrefix(got, "…"), "left clip marker")
		assert.True(t, strings.HasSuffix(got, "…"), "right clip marker")
		assert.NotContains(t, got, "\n")
	})

	t.Run("multibyte text stays valid utf8", func(t *testing.T) {
		text := strings.Repeat("héllo wörld ", 40) + "NEEDLE" + strings.Repeat(" héllo", 40)
		re, _ := CompileQuery("needle", false, false)
		loc := re.FindStringIndex(text)
		got := Snippet(text, loc)
		assert.True(t, utf8.ValidString(got))
		assert.Contains(t, got, "NEEDLE")
	})
}
