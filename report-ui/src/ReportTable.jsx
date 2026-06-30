import React from 'react';

const ReportTable = ({ data }) => {
  if (!data || !data.packages) return <div className="loading">Loading data...</div>;

  const date = new Date(data.timestamp).toLocaleDateString('en-GB');
  const identity = data.router_identity;
  const packages = data.packages;

  // Row definitions - simple and clean
  const rowConfig = [
    { label: 'Speed Test IP', getValue: p => p.tests?.speedtest?.client_ip || 'Check it' },
    { label: 'Download', getValue: p => formatSpeed(p.tests?.download) },
    { label: 'Facebook', getValue: p => formatSpeed(p.tests?.facebook) },
    { label: 'Facebook IP', getValue: p => getTopIp(p.tests?.facebook) },
    { label: 'Fast', getValue: p => formatSpeed(p.tests?.fast) },
    { label: 'Speedtest (Oklaa)', getValue: p => formatSpeed(p.tests?.speedtest) },
    { label: 'YouTube', getValue: p => formatSpeed(p.tests?.youtube) },
    { label: 'YouTube IP', getValue: p => getTopIp(p.tests?.youtube) },
  ];

  function formatSpeed(testObj) {
    if (!testObj) return '-';
    if (testObj.status === 'SUCCESS' && testObj.avg_mbps) {
      return testObj.avg_mbps.toFixed(1);
    }
    // FAILED or SKIPPED or any non-success = "Check it"
    return 'Check it';
  }

  function getTopIp(testObj) {
    if (!testObj || !testObj.top_ips || testObj.top_ips.length === 0) return '-';
    return testObj.top_ips[0].IP;
  }

  // Check if a package has connection issues
  function isConnectionBad(p) {
    return p.connection?.status !== 'CONNECTED';
  }

  return (
    <div className="table-wrapper">
      <table className="report-table">
        <thead>
          <tr className="date-row">
            <th colSpan={packages.length + 1}>{date}</th>
          </tr>
          <tr className="province-row">
            <th colSpan={packages.length + 1}>{identity}</th>
          </tr>
          <tr className="profiles-row">
            <th className="corner-header">Profile / Package</th>
            {packages.map((p, idx) => (
              <th key={idx} className={isConnectionBad(p) ? 'pkg-error' : ''}>
                {p.package_name}
                {isConnectionBad(p) && <span className="conn-badge">Check it</span>}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rowConfig.map((row, rIdx) => (
            <tr key={rIdx}>
              <td className="row-label">{row.label}</td>
              {packages.map((p, cIdx) => {
                const val = row.getValue(p);
                const isBad = val === 'Check it';
                return (
                  <td key={cIdx} className={isBad ? 'cell-error' : 'cell-ok'}>
                    {isBad ? (
                      <span className="check-it">Check it</span>
                    ) : (
                      val
                    )}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
};

export default ReportTable;
