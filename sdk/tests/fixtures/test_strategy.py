from clawquant import log, log_profit, log_status, sleep, storage


def main(exchange, params):
    """测试策略 - 不真正交易，只测试 SDK 全部功能"""

    log("策略启动")
    log_status("初始化中...")

    fast = params.get("fast_period", 5)
    log(f"参数 fast_period = {fast}")

    count = storage.get("run_count", 0)
    count += 1
    storage.set("run_count", count)
    log(f"第 {count} 次运行")

    ticker = exchange.get_ticker()
    if ticker:
        log(f"当前价格: {ticker['last']}")
        log_status(f"价格: {ticker['last']} | 运行次数: {count}")
    else:
        log("行情获取失败（可能是 testnet 未配置）")
        log_status(f"Testnet 模式 | 运行次数: {count}")

    for i in range(3):
        log(f"运行周期 {i + 1}/3")
        log_profit(i * 10.5)
        sleep(2000)

    log("策略正常结束")
