package tourpackage

import "fmt"

// brochureCopy is the locale table for the leaflet. Kept next to the builder
// so EN/AR stay in lockstep and the PDF adapter never hard-codes product words.
type brochureCopy struct {
	labels                                                BrochureLabels
	Duration, Transport, Stay, Destination, Leader, Group string
	Flights, Ground, Coach, Visa, Insurance, AirportMeet  string
	Days, Nights, YourHotel                               string
	ChDepart, ChMadinah, ChMakkah, ChTransfer, ChReturn   string
	ChArrive, ChCity                                      string
	narrFlight, narrFlightNamed, narrRoad                 string
	narrMadinah, narrMakkah, narrTransfer, narrReturn     string
	narrArrive, narrCity                                  string
	And, Comma                                            string
	routeOn, meetLine, visaLine, insureLine, leaderLine   string
	femaleLine, daysLine, hotelTransfers                  string
	worshipMadinah, ritesMakkah, ziyaratLine, kitLine     string
	transferCare, returnClose, arriveCare, cityCare       string
	daysUnfold, metresFrom                                string
	kinds, categories, transports                         map[string]string
	boards, accessBy, routings, intercityBy               map[string]string
	visas, kits, ziyaratBy, cities                        map[string]string
	taglines                                              map[string]string
	footerWith, footerBare                                string
}

func copyFor(lang BrochureLang) brochureCopy {
	if lang == BrochureAR {
		return arCopy
	}
	return enCopy
}

func (c brochureCopy) days(n int) string   { return fmt.Sprintf(c.Days, n) }
func (c brochureCopy) nights(n int) string { return fmt.Sprintf(c.Nights, n) }

func (c brochureCopy) tagline(kind string) string {
	if s := c.taglines[kind]; s != "" {
		return s
	}
	return c.taglines[KindUmrah]
}

func (c brochureCopy) footer(brand string) string {
	if brand == "" {
		return c.footerBare
	}
	return fmt.Sprintf(c.footerWith, brand)
}

func (c brochureCopy) lookup(m map[string]string, key, fallback string) string {
	if key == "" {
		return ""
	}
	if s := m[key]; s != "" {
		return s
	}
	if fallback != "" {
		return fallback
	}
	return key
}

func (c brochureCopy) kind(v string) string      { return c.lookup(c.kinds, v, v) }
func (c brochureCopy) category(v string) string  { return c.lookup(c.categories, v, v) }
func (c brochureCopy) transport(v string) string { return c.lookup(c.transports, v, v) }
func (c brochureCopy) board(v string) string     { return c.lookup(c.boards, v, "") }
func (c brochureCopy) access(v string) string    { return c.lookup(c.accessBy, v, "") }
func (c brochureCopy) routing(v string) string   { return c.lookup(c.routings, v, v) }
func (c brochureCopy) intercity(v string) string { return c.lookup(c.intercityBy, v, v) }
func (c brochureCopy) visa(v string) string      { return c.lookup(c.visas, v, v) }
func (c brochureCopy) kit(v string) string       { return c.lookup(c.kits, v, v) }
func (c brochureCopy) ziyarat(v string) string   { return c.lookup(c.ziyaratBy, v, v) }
func (c brochureCopy) city(v string) string      { return c.lookup(c.cities, v, v) }

var enCopy = brochureCopy{
	labels: BrochureLabels{
		Official: "A quiet guide to your journey", From: "From", PerPerson: "per person",
		Stay: "Your stay", Travel: "Getting there", Included: "Included with care",
		Programme: "Visits & highlights", Itinerary: "Day by day", Prices: "Investment",
		Excluded: "Kindly note", Prepare: "Before you travel", Journey: "The journey", Page: "Page",
		Features: "In this package",
	},
	Duration:        "Duration",
	Transport:       "Travel",
	Stay:            "Nights",
	Destination:     "Destination",
	Leader:          "Tour leader",
	Group:           "Group size",
	Flights:         "Flights",
	Ground:          "Between cities",
	Coach:           "Coach",
	Visa:            "Visa",
	Insurance:       "Health insurance",
	AirportMeet:     "Airport meet & assist",
	Days:            "%d days",
	Nights:          "%d nights",
	YourHotel:       "your hotel",
	ChDepart:        "Departure",
	ChMadinah:       "Madinah · %d nights",
	ChMakkah:        "Makkah · %d nights",
	ChTransfer:      "Between the two cities",
	ChReturn:        "The journey home",
	ChArrive:        "Arrival",
	ChCity:          "%s · %d nights",
	narrFlight:      "The journey begins in the air. We meet you on arrival, settle every document, and keep the first hours unhurried.",
	narrFlightNamed: "The journey begins with %s. We meet you on arrival, settle every document, and transfer you in quiet comfort.",
	narrRoad:        "The group travels by road, with a dedicated coach and a leader beside you from the first kilometre.",
	narrMadinah:     "You pause in the City of the Prophet for %d nights, at %s.",
	narrMakkah:      "You then stay %d nights beside the Sacred Mosque, at %s.",
	narrTransfer:    "Between the two holy cities we continue by %s, luggage and companions kept together.",
	narrReturn:      "The same care that met you at the start sees you home: a composed transfer, documents in order, and a quiet farewell.",
	narrArrive:      "Welcome to %s. We receive the group, settle the first night, and begin at an easy pace.",
	narrCity:        "You spend %d nights in %s, staying at %s.",
	And:             " and ",
	Comma:           ", ",
	routeOn:         "You fly %s on the %s route.",
	meetLine:        "A host is waiting in the arrivals hall, papers already in hand, so the first hour stays unhurried.",
	visaLine:        "Your %s is arranged before you leave home.",
	insureLine:      "Health cover travels with you.",
	leaderLine:      "%s accompanies the group from the first greeting to the last.",
	femaleLine:      "A female guide remains with the sisters throughout.",
	daysLine:        "The programme lasts %s.",
	hotelTransfers:  "Hotel transfers are included, so you are not left to find your way.",
	worshipMadinah:  "Mornings and evenings belong to the Mosque; we keep the hours spacious so prayer comes first.",
	ritesMakkah:     "Ihram, the first Umrah and the nightly rhythm of the Haram are guided without haste, with time to rest between the rites.",
	ziyaratLine:     "Together we visit %s, walking the sites with calm rather than a checklist.",
	kitLine:         "We provide %s so you arrive prepared.",
	transferCare:    "Luggage and companions stay together. The crossing is paced so you arrive ready, not worn from the road.",
	returnClose:     "A composed transfer, documents in order, and a quiet farewell — the same care that met you at the start.",
	arriveCare:      "We receive the group, settle the first night, and begin at an easy pace.",
	cityCare:        "The days are kept spacious: time to arrive, rest, and see the place properly.",
	daysUnfold:      "Those days include %s.",
	metresFrom:      "%d m from the sanctuary",
	footerWith:      "%s  ·  We would be honoured to welcome you.",
	footerBare:      "We would be honoured to welcome you.",
	taglines: map[string]string{
		KindUmrah: "A blessed journey, arranged with care",
		KindHajj:  "Walk the rites in comfort and dignity",
		KindOther: "A journey designed around you",
		KindVisa:  "Entry arranged, priced by type",
	},
	kinds:       map[string]string{KindUmrah: "Umrah", KindHajj: "Hajj", KindOther: "Private tour", KindVisa: "Visa"},
	categories:  map[string]string{"economy": "Economy", "standard": "Standard", "luxury": "Luxury", "personal": "Personal", "boutique": "Boutique", "ramadan_first15": "Ramadan (first 15)", "ramadan_last15": "Ramadan (last 15)", "ramadan_full": "Full Ramadan", "semester": "Semester", "short": "Short", "long": "Long", "special_mujamala": "Mujamala", "special_commercial": "Commercial"},
	transports:  map[string]string{"flight_scheduled": "Scheduled flights", "flight_charter": "Charter flight", "road": "By road"},
	boards:      map[string]string{"bb": "Bed & breakfast", "hb": "Half board", "fb": "Full board", "tabldot": "Table d'hôte", "buffet": "Buffet"},
	accessBy:    map[string]string{"walking": "Walking distance", "shuttle": "Hotel shuttle"},
	routings:    map[string]string{"direct": "Direct", "connecting": "Connecting"},
	intercityBy: map[string]string{"haramain_train": "Haramain train", "bus": "Coach", "private_car": "Private car"},
	visas:       map[string]string{"UMRAH_VISA": "Umrah visa", "TOURIST_VISA": "Tourist visa", "PERSONAL_VISIT": "Personal visit", "HAJJ_VISA": "Hajj visa", "BUSINESS_VISIT": "Business visit"},
	kits:        map[string]string{"ihram": "Ihram", "luggage": "Luggage", "prayer_book": "Prayer book", "zamzam": "Zamzam"},
	ziyaratBy:   map[string]string{"thawr": "Jabal Thawr", "arafat": "Arafat", "muzdalifah": "Muzdalifah", "mina": "Mina", "hira": "Jabal al-Nour (Hira)", "jannat_al_mualla": "Jannat al-Mu'alla", "uhud": "Uhud", "qiblatayn": "Qiblatayn", "seven_mosques": "Seven mosques", "quba": "Quba", "baqi": "Al-Baqi"},
	cities:      map[string]string{"makkah": "Makkah", "madinah": "Madinah", "jeddah": "Jeddah", "transit": "Transit"},
}

var arCopy = brochureCopy{
	labels: BrochureLabels{
		Official: "دليل هادئ لرحلتك", From: "ابتداءً من", PerPerson: "للشخص",
		Stay: "إقامتك", Travel: "الوصول", Included: "ما يشمله البرنامج",
		Programme: "الزيارات والأبرز", Itinerary: "اليوم بيوم", Prices: "الاستثمار",
		Excluded: "يرجى ملاحظة", Prepare: "قبل السفر", Journey: "الرحلة", Page: "صفحة",
		Features: "في هذه الباقة",
	},
	Duration:        "المدة",
	Transport:       "السفر",
	Stay:            "الليالي",
	Destination:     "الوجهة",
	Leader:          "قائد الرحلة",
	Group:           "حجم المجموعة",
	Flights:         "الطيران",
	Ground:          "بين المدن",
	Coach:           "الحافلة",
	Visa:            "التأشيرة",
	Insurance:       "تأمين صحي",
	AirportMeet:     "الاستقبال في المطار",
	Days:            "%d أيام",
	Nights:          "%d ليالٍ",
	YourHotel:       "فندقك",
	ChDepart:        "المغادرة",
	ChMadinah:       "المدينة · %d ليالٍ",
	ChMakkah:        "مكة · %d ليالٍ",
	ChTransfer:      "بين الحرمين",
	ChReturn:        "العودة",
	ChArrive:        "الوصول",
	ChCity:          "%s · %d ليالٍ",
	narrFlight:      "تبدأ الرحلة في الجو. نستقبلك عند الوصول، ونتولى الوثائق، ونُبقي الساعات الأولى هادئة.",
	narrFlightNamed: "تبدأ الرحلة مع %s. نستقبلك عند الوصول ونتولى الوثائق وننقلك براحة هادئة.",
	narrRoad:        "تنتقل المجموعة براً، بحافلة مخصصة وقائد يرافقك من الكيلومتر الأول.",
	narrMadinah:     "تقفون في طيبة %d ليالٍ، في %s.",
	narrMakkah:      "ثم تقيمون %d ليالٍ بجوار المسجد الحرام، في %s.",
	narrTransfer:    "بين الحرمين نواصل ب%s، مع بقاء الأمتعة والرفقة معاً.",
	narrReturn:      "نفس العناية التي استقبلتك في البداية تودّعك: نقل هادئ، وثائق مرتبة، ووداع رقيق.",
	narrArrive:      "أهلاً بك في %s. نستقبل المجموعة ونرتب الليلة الأولى ونبدأ بوتيرة مريحة.",
	narrCity:        "تقضون %d ليالٍ في %s، في %s.",
	And:             " و",
	Comma:           "، ",
	routeOn:         "تسافرون %s على مسار %s.",
	meetLine:        "في صالة الوصول ينتظركم مضيف، والوثائق جاهزة، فتبقى الساعة الأولى هادئة.",
	visaLine:        "تُجهَّز %s قبل مغادرتكم.",
	insureLine:      "والتأمين الصحي يرافقكم.",
	leaderLine:      "يرافق %s المجموعة من أول الترحيب إلى آخره.",
	femaleLine:      "وتبقى مرشدة مع الأخوات طوال الرحلة.",
	daysLine:        "يمتد البرنامج %s.",
	hotelTransfers:  "وتنقّلات الفندق مشمولة، فلا تُتركون لتجدوا الطريق وحدكم.",
	worshipMadinah:  "الصبح والمساء للمسجد؛ نبقي الساعات فسيحة ليأتي الصلاة أولاً.",
	ritesMakkah:     "الإحرام والعمرة الأولى وإيقاع الحرم تُرافق بلا عجلة، مع وقت للراحة بين المناسك.",
	ziyaratLine:     "نزور معاً %s، بهدوء لا بقائمة مستعجلة.",
	kitLine:         "نوفّر %s لتصلوا مستعدين.",
	transferCare:    "تبقى الأمتعة والرفقة معاً. نعبر بوتيرة تصلكم جاهزين لا متعبين من الطريق.",
	returnClose:     "نقل هادئ، وثائق مرتبة، ووداع رقيق — نفس العناية التي استقبلتكم في البداية.",
	arriveCare:      "نستقبل المجموعة ونرتب الليلة الأولى ونبدأ بوتيرة مريحة.",
	cityCare:        "الأيام فسيحة: وقت للوصول والراحة ورؤية المكان على مهل.",
	daysUnfold:      "تشمل تلك الأيام %s.",
	metresFrom:      "%d م من الحرم",
	footerWith:      "%s  ·  يسعدنا أن نرحب بك.",
	footerBare:      "يسعدنا أن نرحب بك.",
	taglines: map[string]string{
		KindUmrah: "رحلة مباركة، نُظّمت بعناية",
		KindHajj:  "أدِّ المناسك براحة ووقار",
		KindOther: "رحلة صُمّمت لك",
		KindVisa:  "تأشيرة مرتّبة، مسعّرة حسب النوع",
	},
	kinds:       map[string]string{KindUmrah: "عمرة", KindHajj: "حج", KindOther: "رحلة خاصة", KindVisa: "تأشيرة"},
	categories:  map[string]string{"economy": "اقتصادي", "standard": "قياسي", "luxury": "فاخر", "personal": "شخصي", "boutique": "بوتيك", "ramadan_first15": "رمضان (الأولى)", "ramadan_last15": "رمضان (الأخيرة)", "ramadan_full": "رمضان كامل", "semester": "فصل دراسي", "short": "قصير", "long": "طويل", "special_mujamala": "مجاملة", "special_commercial": "تجاري"},
	transports:  map[string]string{"flight_scheduled": "رحلات مجدولة", "flight_charter": "رحلة خاصة", "road": "برّاً"},
	boards:      map[string]string{"bb": "إفطار", "hb": "نصف إقامة", "fb": "إقامة كاملة", "tabldot": "مائدة ثابتة", "buffet": "بوفيه"},
	accessBy:    map[string]string{"walking": "مشياً إلى الحرم", "shuttle": "نقل الفندق"},
	routings:    map[string]string{"direct": "مباشر", "connecting": "بترانزيت"},
	intercityBy: map[string]string{"haramain_train": "قطار الحرمين", "bus": "حافلة", "private_car": "سيارة خاصة"},
	visas:       map[string]string{"UMRAH_VISA": "تأشيرة عمرة", "TOURIST_VISA": "تأشيرة سياحية", "PERSONAL_VISIT": "زيارة شخصية", "HAJJ_VISA": "تأشيرة حج", "BUSINESS_VISIT": "زيارة عمل"},
	kits:        map[string]string{"ihram": "إحرام", "luggage": "حقيبة", "prayer_book": "كتاب أدعية", "zamzam": "زمزم"},
	ziyaratBy:   map[string]string{"thawr": "جبل ثور", "arafat": "عرفات", "muzdalifah": "مزدلفة", "mina": "منى", "hira": "جبل النور (حراء)", "jannat_al_mualla": "جنة المعلاة", "uhud": "أحد", "qiblatayn": "القبلتين", "seven_mosques": "المساجد السبعة", "quba": "قباء", "baqi": "البقيع"},
	cities:      map[string]string{"makkah": "مكة", "madinah": "المدينة", "jeddah": "جدة", "transit": "ترانزيت"},
}
