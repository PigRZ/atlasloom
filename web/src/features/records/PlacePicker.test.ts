import { describe, expect, it } from "vitest";
import { amapNavigationURL, buildPlaceSelection } from "./PlacePicker";

describe("map place selection", () => {
  it("creates one precise place payload for weather and navigation", () => {
    const result = buildPlaceSelection({
      name: "示例东站",
      address: "示例省示例市示例区示例路333号",
      longitude: 116.397428,
      latitude: 39.90923,
      adcode: "110101",
      region: "示例省示例市示例区"
    });
    expect(result).toMatchObject({
      name: "示例东站",
      longitude: 116.397428,
      latitude: 39.90923,
      weatherAdcode: "110101"
    });
    expect(String(result.navigationURL)).toContain("uri.amap.com/navigation");
  });

  it("uses the official AMap URI navigation target", () => {
    const url = amapNavigationURL(104.1, 30.6, "测试地点");
    expect(url).toContain("to=104.1%2C30.6%2C%E6%B5%8B%E8%AF%95%E5%9C%B0%E7%82%B9");
    expect(url).toContain("coordinate=gaode");
  });
});
