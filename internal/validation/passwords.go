package validation

import "strings"

var commonPasswords = map[string]bool{}

func init() {
	list := `123456 password 123456789 12345678 12345 qwerty 1234567 111111 123123
1234567890 1234 iloveyou 000000 dragon abc123 monkey letmein 696969 shadow master
666666 qwertyuiop 123321 mustang 1234567891 michael 654321 superman 1qaz2wsx 7777777
121212 000000 qazwsx 123qwe killer trustno1 jordan jennifer zxcvbnm asdfgh hunter
buster soccer harley batman andrew tigger sunshine iloveu 2000 charlie robert thomas
hockey ranger daniel starwars klaster 112233 george computer michelle jessica pepper
1111 zxcvbn 555555 11111111 131313 freedom 777777 pass maggie 159753 aaaaaa ginger
princess joshua cheese amanda summer love ashley nicole chelsea biteme matthew access
yankees 987654321 dallas austin thunder taylor matrix mobilemail mom monitor monitoring
montana moon moscow motdepasse azerty azertyuiop soleil bonjour chouchou coucou
nintendo loulou jetaime marseille bienvenue camille doudou 123soleil administrateur
motdepasse1 password1 password123 passw0rd p@ssw0rd admin admin123 root toor welcome
welcome1 login guest test test123 changeme secret qwerty123 1q2w3e4r 1q2w3e4r5t
abcd1234 a1b2c3d4 letmein1 football baseball qwe123 asd123 zaq12wsx qwertyui`

	for _, word := range strings.Fields(list) {
		commonPasswords[word] = true
	}
}

func IsCommonPassword(candidate string) bool {
	return commonPasswords[strings.ToLower(strings.TrimSpace(candidate))]
}
