- log
- utils
- pb
- common
- crypto
  - peers (common, crypto)
    - poset (log, common, crypto, peers)
      - proxy (common, poset)	
      - net (log, utils, common, poset)
        - dummy (common, crypto, poset, proxy)
          - node (log, common, crypto, peers, poset, proxy, net, dummy)
            - difftool (node, poset)
            - service (node)
              - lachesis (log, crypto, peers, poset, proxy, net, node, service)
                - mobile (crypto, peers, poset, proxy, node, lachesis)
- version
