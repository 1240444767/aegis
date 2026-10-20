package response

import "testing"

func TestRewriteUserIDOnlyTouchesCallerIDFields(t *testing.T) {
	in := `{"id":42,"userId":42,"user_id":42,"uid":42,"requesterUserId":42,"inviterUserId":7,"orderId":42,` +
		`"userIds":[42],"note":"{\"userId\":42}","list":[{"userId":42},{"userId":420}],"x":{"uploadedById":42}}`
	want := `{"id":42,"userId":4829173056482917,"user_id":4829173056482917,"uid":4829173056482917,"requesterUserId":4829173056482917,"inviterUserId":7,"orderId":42,` +
		`"userIds":[42],"note":"{\"userId\":42}","list":[{"userId":4829173056482917},{"userId":420}],"x":{"uploadedById":4829173056482917}}`
	if got := string(RewriteUserID([]byte(in), "42", "4829173056482917")); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
