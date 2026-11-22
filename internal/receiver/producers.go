package receiver

import "strings"

// Public name/address pairs from the explorer's pillar directory. An explicit edit
// (including an empty address) takes precedence over the directory.
// https://github.com/0x3639/explorer.zenon.info/blob/0abfad6f822b28f285d7584153f28b4e77686976/src/lib/constants.ts
var explorerProducers = map[string]string{
	"0x3639.com":      "z1qpqn7uqlqyqeuf3z5sudvk2tgg2ghwa9pyq5ny",
	"12n11":           "z1qqu775znqmk8qlyqt36t6qnjvulqgmzt0vnlga",
	"anvil":           "z1qr44r4fxhs33v7jusreurda7n60xy4gqvg8j9u",
	"deeznnutz.com":   "z1qr9dd6qmzwr8sazns24lna8zz5e8208nqq2hcx",
	"elderz":          "z1qr5kzrlkw8w7ddfx63rrv6a46n03kncpmvhct8",
	"mariposa01":      "z1qpza2k4fldpwsjrw0ae27ywnfnsc352sfed2e0",
	"megalith":        "z1qrztagl9rukq3ltdflnvg4zrvpfp84mydfejk9",
	"moonbaze":        "z1qr68wfxwj40p24ujuhvclzxh3yksdnqvv9l9m3",
	"nexus":           "z1qpz5mjcyl8kftfk68l7x5afe663sh6xalts8uk",
	"nomlabz.org":     "z1qqjx9zdunxxuvkqwjfkac0jvnrzwu45ey8lhyl",
	"stakeserve":      "z1qqm9ld4vgx6ntv96qf5gyxqenzune7n3aadg4h",
	"stark":           "z1qzdwhpqwtz5yhrp7nuk7cl6zhjp3ywn57l3l9v",
	"sultanofstaking": "z1qrs6u4vmgxkgqcamq7hlfqm9mzad45u66vntfx",
	"tapwoot":         "z1qz7rl42ytj7j5kyqn6dw38hds3pwru38y3p7r0",
	"time":            "z1qqrjdnzf2tuvxnp7dn54l7t6k6u60k5m5nknvg",
	"wotgasfees":      "z1qr9yer2qlhthg0c5jh5j6pzcc8c8ta8tdljk57",
	"zeno":            "z1qz855vhcq70zg22h035kuxk7htummvt5yc98yh",
	"zenonorg":        "z1qpfs9pef5sht6e7jzc4utmnnmt9gpy0e7tpgs0",
	"zenonorg2":       "z1qzfq7urv5ykn9dnny9pe0z7dkssxqjn97wrv42",
	"zenonorg3":       "z1qpuxlt357nsy6kuvqhvx2vdv0lfyw576a005me",
}

func (n node) publicProducer() string {
	if n.Producer != nil {
		return *n.Producer
	}
	return explorerProducers[strings.ToLower(n.Name)]
}
