import {
  birthdayText,
  contactMessages,
  type ContactValues,
  daysIn,
  normalizePhone,
  validateContact,
  validateLinkURL,
} from "./contact-validation";

const empty: ContactValues = {
  emailVisibility: "only_me",
  phone: "",
  phoneVisibility: "only_me",
  month: "",
  day: "",
  birthdayVisibility: "only_me",
};

describe("validateContact", () => {
  it("accepts nothing set", () => {
    expect(validateContact(empty)).toEqual({});
  });

  it.each(["+8801712345678", "+880 1712-345678", "+12"])(
    "accepts the phone %j",
    (phone) => {
      expect(validateContact({ ...empty, phone })).toEqual({});
    },
  );

  it.each([
    "8801712345678",
    "+0123456",
    "+1",
    "+88017ABC",
    `+${"1".repeat(16)}`,
    "+1 (555) 1234567",
  ])("refuses the phone %j", (phone) => {
    expect(validateContact({ ...empty, phone })).toEqual({
      phone: contactMessages.phoneInvalid,
    });
  });

  it("accepts 29 February and 31 December", () => {
    expect(validateContact({ ...empty, month: "2", day: "29" })).toEqual({});
    expect(validateContact({ ...empty, month: "12", day: "31" })).toEqual({});
  });

  it.each([
    ["2", "30"],
    ["4", "31"],
    ["13", "1"],
    ["1", "0"],
  ])("refuses month %s day %s", (month, day) => {
    expect(validateContact({ ...empty, month, day })).toEqual({
      birthday: contactMessages.birthdayInvalid,
    });
  });

  it("needs both month and day, or neither", () => {
    expect(validateContact({ ...empty, month: "5" })).toEqual({
      birthday: contactMessages.birthdayIncomplete,
    });
    expect(validateContact({ ...empty, day: "5" })).toEqual({
      birthday: contactMessages.birthdayIncomplete,
    });
  });
});

describe("validateLinkURL", () => {
  it.each([
    "https://example.org",
    " https://scholar.example.org/ada ",
    "HTTPS://example.org/x",
  ])("accepts %j", (url) => {
    expect(validateLinkURL(url)).toBeUndefined();
  });

  it("refuses http and links without a scheme", () => {
    expect(validateLinkURL("http://example.org")).toBe(
      contactMessages.linkNotHttps,
    );
    expect(validateLinkURL("example.org")).toBe(contactMessages.linkNotHttps);
    expect(validateLinkURL("javascript:alert(1)")).toBe(
      contactMessages.linkNotHttps,
    );
  });

  it("refuses an empty, broken or too long link", () => {
    expect(validateLinkURL("  ")).toBe(contactMessages.linkRequired);
    expect(validateLinkURL("https://")).toBe(contactMessages.linkInvalid);
    expect(validateLinkURL("https://example.org/a b")).toBe(
      contactMessages.linkInvalid,
    );
    expect(validateLinkURL(`https://${"a".repeat(293)}`)).toBe(
      contactMessages.linkTooLong,
    );
  });
});

describe("helpers", () => {
  it("knows the days of each month, with 29 for February", () => {
    expect([1, 2, 4, 12].map(daysIn)).toEqual([31, 29, 30, 31]);
  });

  it("removes spaces and hyphens from a phone number", () => {
    expect(normalizePhone(" +880 1712-345678 ")).toBe("+8801712345678");
  });

  it("shows a birthday without a year", () => {
    expect(birthdayText({ month: 2, day: 29 })).toBe("29 February");
  });
});
