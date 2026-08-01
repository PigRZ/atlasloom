#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const API_ROOT = "https://restapi.amap.com";
const RETRYABLE_INFO_CODES = new Set([
  "10004",
  "10020",
  "10021",
  "10022",
  "10044",
]);

function fail(message) {
  process.stderr.write(`${message}\n`);
  process.exit(2);
}

function usage(stream = process.stderr) {
  stream.write(`用法：
  amap.mjs place --keywords TEXT [--region CITY_OR_ADCODE]
  amap.mjs reverse --location LNG,LAT
  amap.mjs route --mode drive|walk|bicycle|transit --origin LNG,LAT --destination LNG,LAT [--city CITY] [--destination-city CITY]
  amap.mjs weather --adcode SIX_DIGITS [--extensions base|all]
`);
}

function parseArguments(values) {
  const result = {};
  for (let index = 0; index < values.length; index += 2) {
    const flag = values[index];
    const value = values[index + 1];
    if (!flag?.startsWith("--") || value === undefined || value.startsWith("--")) {
      fail(`参数格式错误：${flag ?? ""}`);
    }
    const key = flag.slice(2);
    if (Object.hasOwn(result, key)) {
      fail(`参数重复：${flag}`);
    }
    result[key] = value;
  }
  return result;
}

function requireOnly(options, allowed) {
  for (const key of Object.keys(options)) {
    if (!allowed.includes(key)) {
      fail(`未知参数：--${key}`);
    }
  }
}

function required(options, key) {
  const value = options[key]?.trim();
  if (!value) fail(`缺少 --${key}`);
  return value;
}

function validateCoordinate(value, label) {
  const parts = value.split(",");
  if (parts.length !== 2) fail(`${label} 必须是 经度,纬度`);
  const longitude = Number(parts[0]);
  const latitude = Number(parts[1]);
  if (
    !Number.isFinite(longitude) ||
    !Number.isFinite(latitude) ||
    longitude < -180 ||
    longitude > 180 ||
    latitude < -90 ||
    latitude > 90
  ) {
    fail(`${label} 超出合法经纬度范围`);
  }
  return `${longitude},${latitude}`;
}

function findProjectRoot(start) {
  let current = path.resolve(start);
  while (true) {
    if (fs.existsSync(path.join(current, "config", "project.env.example"))) {
      return current;
    }
    const parent = path.dirname(current);
    if (parent === current) return "";
    current = parent;
  }
}

function envValue(file, target) {
  if (!fs.existsSync(file)) return "";
  for (const rawLine of fs.readFileSync(file, "utf8").split(/\r?\n/u)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) continue;
    const separator = line.indexOf("=");
    if (separator < 1) continue;
    if (line.slice(0, separator).trim() !== target) continue;
    let value = line.slice(separator + 1).trim();
    if (
      value.length >= 2 &&
      ((value.startsWith('"') && value.endsWith('"')) ||
        (value.startsWith("'") && value.endsWith("'")))
    ) {
      value = value.slice(1, -1);
    }
    return value;
  }
  return "";
}

function webServiceKey() {
  const fromEnvironment = process.env.AMAP_WEBSERVICE_KEY?.trim();
  if (fromEnvironment) return fromEnvironment;
  const root = findProjectRoot(process.cwd());
  if (!root) fail("无法定位项目根目录，请设置 AMAP_WEBSERVICE_KEY。");
  const fromProject = envValue(
    path.join(root, "config", "project.env"),
    "AMAP_WEBSERVICE_KEY",
  );
  if (!fromProject) {
    fail("未配置 AMAP_WEBSERVICE_KEY；请填写被 Git 忽略的 config/project.env。");
  }
  return fromProject;
}

function requestDefinition(command, options) {
  switch (command) {
    case "place": {
      requireOnly(options, ["keywords", "region"]);
      const parameters = {
        keywords: required(options, "keywords"),
        extensions: "all",
        offset: "20",
        page: "1",
      };
      if (options.region?.trim()) {
        parameters.city = options.region.trim();
        parameters.citylimit = "true";
      }
      return { endpoint: "/v3/place/text", parameters };
    }
    case "reverse": {
      requireOnly(options, ["location"]);
      return {
        endpoint: "/v3/geocode/regeo",
        parameters: {
          location: validateCoordinate(required(options, "location"), "location"),
          extensions: "all",
          radius: "1000",
        },
      };
    }
    case "route": {
      requireOnly(options, [
        "mode",
        "origin",
        "destination",
        "city",
        "destination-city",
      ]);
      const mode = required(options, "mode");
      const endpoints = {
        drive: "/v3/direction/driving",
        walk: "/v3/direction/walking",
        bicycle: "/v4/direction/bicycling",
        transit: "/v3/direction/transit/integrated",
      };
      if (!Object.hasOwn(endpoints, mode)) {
        fail("--mode 必须是 drive、walk、bicycle 或 transit");
      }
      const parameters = {
        origin: validateCoordinate(required(options, "origin"), "origin"),
        destination: validateCoordinate(
          required(options, "destination"),
          "destination",
        ),
      };
      if (mode === "transit") {
        parameters.city = required(options, "city");
        if (options["destination-city"]?.trim()) {
          parameters.cityd = options["destination-city"].trim();
        }
      } else if (options.city || options["destination-city"]) {
        fail("--city 和 --destination-city 只用于 transit");
      }
      return { endpoint: endpoints[mode], parameters };
    }
    case "weather": {
      requireOnly(options, ["adcode", "extensions"]);
      const adcode = required(options, "adcode");
      if (!/^\d{6}$/u.test(adcode)) fail("--adcode 必须是六位数字");
      const extensions = options.extensions?.trim() || "all";
      if (!["base", "all"].includes(extensions)) {
        fail("--extensions 必须是 base 或 all");
      }
      return {
        endpoint: "/v3/weather/weatherInfo",
        parameters: { city: adcode, extensions },
      };
    }
    default:
      usage();
      fail(`未知命令：${command ?? ""}`);
  }
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

async function requestAmap(endpoint, parameters, key) {
  const url = new URL(endpoint, API_ROOT);
  for (const [name, value] of Object.entries(parameters)) {
    url.searchParams.set(name, value);
  }
  url.searchParams.set("key", key);

  let lastError;
  for (let attempt = 0; attempt < 3; attempt += 1) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 15_000);
    try {
      const response = await fetch(url, {
        headers: { Accept: "application/json", "User-Agent": "my-travel-amap-skill/1" },
        signal: controller.signal,
      });
      const body = await response.text();
      let data;
      try {
        data = JSON.parse(body);
      } catch {
        throw new Error(`高德返回了非 JSON 响应（HTTP ${response.status}）`);
      }
      if (response.status === 429 || response.status >= 500) {
        throw new Error(`高德服务暂时不可用（HTTP ${response.status}）`);
      }
      if (!response.ok) {
        throw new Error(`高德请求失败（HTTP ${response.status}）`);
      }
      const bicycleResponse = endpoint === "/v4/direction/bicycling";
      const succeeded = bicycleResponse
        ? Number(data.errcode) === 0
        : String(data.status) === "1";
      if (!succeeded) {
        const code = String(
          bicycleResponse ? (data.errcode ?? "unknown") : (data.infocode ?? "unknown"),
        );
        const message = bicycleResponse ? data.errmsg : data.info;
        const error = new Error(`高德请求失败（错误码 ${code}）：${message ?? "未知错误"}`);
        if (RETRYABLE_INFO_CODES.has(code)) {
          throw error;
        }
        throw Object.assign(error, { permanent: true });
      }
      return data;
    } catch (error) {
      lastError = error;
      if (error?.permanent || attempt === 2) throw error;
      await delay(500 * 2 ** attempt);
    } finally {
      clearTimeout(timer);
    }
  }
  throw lastError;
}

const [command, ...argumentValues] = process.argv.slice(2);
if (["help", "--help", "-h"].includes(command) && argumentValues.length === 0) {
  usage(process.stdout);
  process.exit(0);
}
const options = parseArguments(argumentValues);
const { endpoint, parameters } = requestDefinition(command, options);
const key = webServiceKey();

try {
  const result = await requestAmap(endpoint, parameters, key);
  const safe = JSON.stringify(result, null, 2).split(key).join("[REDACTED]");
  process.stdout.write(`${safe}\n`);
} catch (error) {
  process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
  process.exit(1);
}
