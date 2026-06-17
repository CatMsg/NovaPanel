package sub

import "testing"

func TestSameSubscriptionSource(t *testing.T) {
	self := "https://la.mile.news:2096/sub/aggregate"

	cases := []struct {
		name   string
		left   string
		right  string
		expect bool
	}{
		{
			name:   "exact match",
			left:   "https://la.mile.news:2096/sub/aggregate",
			right:  self,
			expect: true,
		},
		{
			name:   "trailing slash",
			left:   "https://la.mile.news:2096/sub/aggregate/",
			right:  self,
			expect: true,
		},
		{
			name:   "different host",
			left:   "https://other.example.com:2096/sub/aggregate",
			right:  self,
			expect: false,
		},
		{
			name:   "different path",
			left:   "https://la.mile.news:2096/sub/all",
			right:  self,
			expect: false,
		},
	}

	for _, tc := range cases {
		if got := sameSubscriptionSource(tc.left, tc.right); got != tc.expect {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.expect)
		}
	}
}
